package modbus

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
	"modbus-hub-go/internal/models"
)

const (
	maxRetries = 3
	retryWait  = 250 * time.Millisecond
)

// FrameHook 报文钩子：把每一帧的原始字节交给外部记录。
// note 写这一帧的状态说明（正常为空；超时、CRC 错、异常响应时写明原因）。
type FrameHook func(dir FrameDir, data []byte, note string)

// RTUClient 单通道 Modbus RTU 主站（自建协议栈，不依赖第三方 Modbus 库）
type RTUClient struct {
	portName  string
	mode      *serial.Mode
	timeout   time.Duration
	port      serial.Port
	rxCh      chan byte
	stopCh    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	txCount   int64
	rxCount   int64
	// hookMu 单独保护 onFrame：transaction 整段持着 mu，
	// 钩子里再抢 mu 就是死锁，所以不能复用
	hookMu  sync.Mutex
	onFrame FrameHook
}

// NewRTUClient 依据通道配置构造客户端
func NewRTUClient(cfg models.ChannelConfig) *RTUClient {
	dataBits := cfg.DataBits
	if dataBits != 7 && dataBits != 8 {
		dataBits = 8
	}
	timeout := time.Duration(cfg.ReadTimeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 1000 * time.Millisecond
	}
	return &RTUClient{
		portName: cfg.PortName,
		mode: &serial.Mode{
			BaudRate: cfg.BaudRate,
			DataBits: dataBits,
			Parity:   toSerialParity(cfg.Parity),
			StopBits: toSerialStopBits(cfg.StopBits),
		},
		timeout: timeout,
	}
}

func toSerialParity(p int) serial.Parity {
	switch p {
	case models.ParityOdd:
		return serial.OddParity
	case models.ParityEven:
		return serial.EvenParity
	case models.ParityMark:
		return serial.MarkParity
	case models.ParitySpace:
		return serial.SpaceParity
	default:
		return serial.NoParity
	}
}

func toSerialStopBits(s int) serial.StopBits {
	switch s {
	case models.StopBitsTwo:
		return serial.TwoStopBits
	case 3:
		return serial.OnePointFiveStopBits
	default:
		return serial.OneStopBit
	}
}

// Connect 打开串口并启动接收协程
func (c *RTUClient) Connect() error {
	port, err := serial.Open(c.portName, c.mode)
	if err != nil {
		return fmt.Errorf("打开串口 %s 失败: %w", c.portName, err)
	}
	c.port = port
	c.rxCh = make(chan byte, 8192)
	c.stopCh = make(chan struct{})
	go c.readLoop()
	return nil
}

// Close 关闭串口
func (c *RTUClient) Close() error {
	var err error
	c.closeOnce.Do(func() {
		if c.stopCh != nil {
			close(c.stopCh)
		}
		if c.port != nil {
			err = c.port.Close()
		}
	})
	return err
}

func (c *RTUClient) IsConnected() bool { return c.port != nil }

func (c *RTUClient) TxCount() int64 { return atomic.LoadInt64(&c.txCount) }
func (c *RTUClient) RxCount() int64 { return atomic.LoadInt64(&c.rxCount) }

// SetFrameHook 设置报文钩子。必须在 Connect 之前调用，
// 钩子本身只做记录，抛出去的活儿交给调用方，协议栈不依赖它。
func (c *RTUClient) SetFrameHook(h FrameHook) {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()
	c.onFrame = h
}

// emitFrame 上报一帧；钩子为空时直接返回，零开销
func (c *RTUClient) emitFrame(dir FrameDir, data []byte, note string) {
	c.hookMu.Lock()
	h := c.onFrame
	c.hookMu.Unlock()
	if h == nil {
		return
	}
	// 复制一份再交出去：底层切片可能被后续读写复用，外部异步渲染时不能拿着它
	buf := make([]byte, len(data))
	copy(buf, data)
	h(dir, buf, note)
}

func (c *RTUClient) readLoop() {
	buf := make([]byte, 256)
	for {
		n, err := c.port.Read(buf)
		for i := 0; i < n; i++ {
			select {
			case c.rxCh <- buf[i]:
			case <-c.stopCh:
				return
			}
		}
		if err != nil {
			return
		}
		if n == 0 {
			time.Sleep(2 * time.Millisecond)
		}
	}
}

func (c *RTUClient) drain() {
	for {
		select {
		case <-c.rxCh:
		case <-c.stopCh:
			return
		default:
			return
		}
	}
}

// readExact 读满 n 字节。超时或串口关闭时也要把已收到的部分返回去——
// 排查现场问题时，"从机回了 3 个字节就断了"比干巴巴一句"超时"有用得多。
func (c *RTUClient) readExact(n int, timeout time.Duration) ([]byte, error) {
	out := make([]byte, 0, n)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(out) < n {
		select {
		case b := <-c.rxCh:
			out = append(out, b)
		case <-timer.C:
			return out, fmt.Errorf("响应超时（收到 %d/%d 字节）", len(out), n)
		case <-c.stopCh:
			return out, errors.New("串口已关闭")
		}
	}
	return out, nil
}

// transaction 发一帧、收一帧；异常响应不重试，超时/CRC 错误最多重试 3 次
func (c *RTUClient) transaction(req []byte, respLen int) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryWait)
		}
		select {
		case <-c.stopCh:
			return nil, errors.New("串口已关闭")
		default:
		}

		c.drain()
		atomic.AddInt64(&c.txCount, 1)
		retryMark := ""
		if attempt > 0 {
			retryMark = fmt.Sprintf("第 %d 次重试", attempt+1)
		}
		c.emitFrame(FrameTx, req, retryMark)
		if _, err := c.port.Write(req); err != nil {
			lastErr = fmt.Errorf("发送失败: %w", err)
			c.emitFrame(FrameTx, req, "发送失败："+err.Error())
			continue
		}

		resp, err := c.readExact(respLen, c.timeout)
		if err != nil {
			lastErr = err
			// 残帧也要记下来：从机回了半截就断了，是最典型的现场故障之一
			c.emitFrame(FrameRx, resp, err.Error())
			continue
		}
		atomic.AddInt64(&c.rxCount, 1)

		if !checkCRC(resp) {
			lastErr = errors.New("CRC 校验失败")
			c.emitFrame(FrameRx, resp, "CRC 校验失败，已丢弃并重发")
			continue
		}
		if resp[0] != req[0] {
			lastErr = fmt.Errorf("响应从机地址不符（期望 %d，收到 %d）", req[0], resp[0])
			c.emitFrame(FrameRx, resp, lastErr.Error())
			continue
		}
		if resp[1]&0x80 != 0 {
			code := byte(0)
			if len(resp) > 2 {
				code = resp[2]
			}
			err = fmt.Errorf("从机返回异常 0x%02X：%s", resp[1], exceptionText(code))
			// 异常响应不重试，但必须记下来——这是从机在明说"你这条请求我不认"
			c.emitFrame(FrameRx, resp, err.Error())
			return nil, err
		}
		if resp[1] != req[1] {
			lastErr = fmt.Errorf("响应功能码不符（期望 0x%02X，收到 0x%02X）", req[1], resp[1])
			c.emitFrame(FrameRx, resp, lastErr.Error())
			continue
		}
		c.emitFrame(FrameRx, resp, "")
		return resp, nil
	}
	return nil, lastErr
}

func exceptionText(code byte) string {
	switch code {
	case 0x01:
		return "非法功能码"
	case 0x02:
		return "非法数据地址"
	case 0x03:
		return "非法数据值"
	case 0x04:
		return "从机设备故障"
	case 0x05:
		return "确认（需等待）"
	case 0x06:
		return "从机忙"
	case 0x08:
		return "存储奇偶性错误"
	case 0x0A:
		return "网关路径不可用"
	case 0x0B:
		return "网关目标设备无响应"
	default:
		return fmt.Sprintf("未知异常码 0x%02X", code)
	}
}

// ReadHoldingRegisters 功能码 03
func (c *RTUClient) ReadHoldingRegisters(slave byte, start, count uint16) ([]uint16, error) {
	if count == 0 || count > MaxReadRegisters {
		return nil, fmt.Errorf("读取数量非法：%d", count)
	}
	req := appendCRC([]byte{
		slave, models.FCReadHoldingRegisters,
		byte(start >> 8), byte(start),
		byte(count >> 8), byte(count),
	})
	resp, err := c.transaction(req, 5+int(count)*2)
	if err != nil {
		return nil, err
	}
	if int(resp[2]) != int(count)*2 {
		return nil, errors.New("响应字节数与请求数量不符")
	}
	regs := make([]uint16, count)
	for i := 0; i < int(count); i++ {
		regs[i] = uint16(resp[3+2*i])<<8 | uint16(resp[4+2*i])
	}
	return regs, nil
}

// WriteSingleRegister 功能码 06
func (c *RTUClient) WriteSingleRegister(slave byte, addr, value uint16) error {
	req := appendCRC([]byte{
		slave, models.FCWriteSingleRegister,
		byte(addr >> 8), byte(addr),
		byte(value >> 8), byte(value),
	})
	_, err := c.transaction(req, 8)
	return err
}

// WriteMultipleRegisters 功能码 16
func (c *RTUClient) WriteMultipleRegisters(slave byte, addr uint16, regs []uint16) error {
	if len(regs) == 0 {
		return nil
	}
	if len(regs) > MaxWriteRegisters {
		return fmt.Errorf("一次最多写入 %d 个寄存器", MaxWriteRegisters)
	}
	frame := []byte{
		slave, models.FCWriteMultipleRegisters,
		byte(addr >> 8), byte(addr),
		byte(len(regs) >> 8), byte(len(regs)),
		byte(len(regs) * 2),
	}
	for _, r := range regs {
		frame = append(frame, byte(r>>8), byte(r))
	}
	_, err := c.transaction(appendCRC(frame), 8)
	return err
}

// WriteTag 按点的功能码写入
func (c *RTUClient) WriteTag(tag models.TagPoint, value any) error {
	switch tag.FunctionCode {
	case models.FCWriteSingleRegister:
		return c.WriteSingleRegister(tag.SlaveID, tag.Address, uint16(uint32(ToFloat(value))&0xFFFF))
	case models.FCWriteMultipleRegisters:
		return c.WriteMultipleRegisters(tag.SlaveID, tag.Address, EncodeRegisters(value, tag))
	default:
		return fmt.Errorf("当前点为只读（功能码 03），不可写入")
	}
}

// ReadTags 按块读取点表，返回与输入一一对应的值
func (c *RTUClient) ReadTags(tags []models.TagPoint) []models.TagValue {
	if len(tags) == 0 {
		return nil
	}
	if c.port == nil {
		out := make([]models.TagValue, 0, len(tags))
		for _, t := range tags {
			out = append(out, models.FailedValue(t.ID, "通道未连接"))
		}
		return out
	}

	out := make([]models.TagValue, 0, len(tags))
	for _, b := range SplitReadBlocks(tags) {
		regs, err := c.ReadHoldingRegisters(b.SlaveID, b.Start, b.Count)
		if err != nil {
			for _, t := range b.Tags {
				out = append(out, models.FailedValue(t.ID, err.Error()))
			}
			continue
		}
		for _, t := range b.Tags {
			out = append(out, DecodeFromBlock(t, regs, b.Start))
		}
	}
	return out
}

// DecodeFromBlock 从块读取结果中解出单个点的值
func DecodeFromBlock(tag models.TagPoint, regs []uint16, blockStart uint16) models.TagValue {
	result := models.TagValue{TagID: tag.ID, Timestamp: models.Now(), TimestampMs: models.NowMs()}
	offset := int(tag.Address) - int(blockStart)
	if offset < 0 || offset+tag.RegisterCount() > len(regs) {
		result.Quality = "Bad"
		result.ErrorMessage = "块读取范围不足"
		result.DisplayValue = "ERR"
		return result
	}
	slice := make([]uint16, tag.RegisterCount())
	copy(slice, regs[offset:offset+tag.RegisterCount()])
	raw := ParseRegisters(slice, tag)
	numeric := ToFloat(raw)*tag.Scale + tag.Offset
	result.RawValue = raw
	result.NumericValue = &numeric
	result.DisplayValue = FormatValue(raw, tag)
	result.Registers = slice
	result.HexValue, result.BinValue = BaseView(raw, tag.DataType)
	result.Quality = "Good"
	return result
}
