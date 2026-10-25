package modbus

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"modbus-hub-go/internal/models"
)

const maxRetries = 3

// retryWait 两次重试之间的间隔。
// 刻意做成变量而不是常量：测试里要把它调短，
// 不然每个"重试三次"的用例都得真等上几百毫秒。
var retryWait = 250 * time.Millisecond

// FrameHook 报文钩子：把每一帧的原始字节交给外部记录。
// note 写这一帧的状态说明（正常为空；超时、CRC 错、异常响应时写明原因）。
type FrameHook func(dir FrameDir, data []byte, note string)

// Master Modbus 主站。
//
// 抽这一层的全部理由是一句话：Modbus 的 PDU 在 RTU 与 TCP 上逐字节相同。
// 所以功能码怎么拼、异常怎么认、超时要不要重试，这些逻辑只需要写一遍；
// 介质相关的两件事交给别人——字节怎么进出是 Transport 的事，
// ADU 怎么加壳（加从机地址+CRC 还是加 MBAP 头）是 Framing 的事。
type Master struct {
	tx      Transport
	fr      Framing
	timeout time.Duration

	mu      sync.Mutex
	txCount int64
	rxCount int64

	// hookMu 单独保护 onFrame：transaction 整段持着 mu，
	// 钩子里再抢 mu 就是死锁，所以不能复用
	hookMu  sync.Mutex
	onFrame FrameHook
}

// NewMaster 按通道配置构造主站：串口走 RTU，网口走 TCP。
// 整个程序里只有通道服务启动时调它，介质选择在这里一次性定死。
func NewMaster(cfg models.ChannelConfig) *Master {
	timeout := time.Duration(cfg.ReadTimeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 1000 * time.Millisecond
	}
	if cfg.ConnType.IsTCP() {
		return newMaster(NewTCPTransport(cfg.Host, cfg.Port), &TCPFraming{}, timeout)
	}
	return newMaster(NewRTUTransport(cfg), RTUFraming{}, timeout)
}

func newMaster(tx Transport, fr Framing, timeout time.Duration) *Master {
	return &Master{tx: tx, fr: fr, timeout: timeout}
}

// Protocol 当前走的协议名，写日志用："RTU" / "TCP"
func (m *Master) Protocol() string { return m.fr.Protocol() }

// Endpoint 连接目标文本，写日志用："COM1" / "192.168.1.10:502"
func (m *Master) Endpoint() string { return m.tx.Endpoint() }

// Connect 建立链路
func (m *Master) Connect() error { return m.tx.Open() }

// Close 断开链路
func (m *Master) Close() error { return m.tx.Close() }

// IsConnected 链路是否可用
func (m *Master) IsConnected() bool { return m.tx.IsOpen() }

func (m *Master) TxCount() int64 { return atomic.LoadInt64(&m.txCount) }
func (m *Master) RxCount() int64 { return atomic.LoadInt64(&m.rxCount) }

// SetFrameHook 设置报文钩子。必须在 Connect 之前调用，
// 钩子本身只做记录，抛出去的活儿交给调用方，协议栈不依赖它。
func (m *Master) SetFrameHook(h FrameHook) {
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	m.onFrame = h
}

// emitFrame 上报一帧；钩子为空时直接返回，零开销
func (m *Master) emitFrame(dir FrameDir, data []byte, note string) {
	m.hookMu.Lock()
	h := m.onFrame
	m.hookMu.Unlock()
	if h == nil {
		return
	}
	// 复制一份再交出去：底层切片可能被后续读写复用，外部异步渲染时不能拿着它
	buf := make([]byte, len(data))
	copy(buf, data)
	h(dir, buf, note)
}

// readResponse 读一帧响应。
//
// 两步走：先读够头部，按头里的长度字段把剩下多少字节算出来，再补齐。
// 读不满时把已到手的字节连同错误一起交出去——残帧是排查
// "从机回了半截就断"的唯一线索，丢掉就再也没了。
func (m *Master) readResponse(pdu []byte) ([]byte, error) {
	headLen := m.fr.Head()
	frame, err := m.tx.Read(headLen, m.timeout)
	if err != nil {
		return frame, err
	}
	if len(frame) < headLen {
		return frame, fmt.Errorf("响应超时（收到 %d/%d 字节）", len(frame), headLen)
	}
	total := m.fr.Total(frame[:headLen])
	if total <= len(frame) {
		// 头部认不出来（从机回了别的功能码之类）时按请求的期望长度兜底，
		// 至少能把这条废帧读完整，界面上才看得到它到底说了什么
		if want := m.fr.Expected(pdu); want > len(frame) {
			total = want
		}
	}
	if total > len(frame) {
		rest, rerr := m.tx.Read(total-len(frame), m.timeout)
		frame = append(frame, rest...)
		if rerr != nil {
			// 报的是整帧的进度，不是第二次读的计划数——现场关心的是"帧收到了几分之几"
			err = fmt.Errorf("响应超时（收到 %d/%d 字节）", len(frame), total)
		}
	}
	return frame, err
}

// transaction 发一帧、收一帧，返回响应的数据域。
//
// 超时与校验错误最多重试 maxRetries 次；异常响应是从机明确回答"不认这条请求"，
// 重试多少次都一样，直接返回。每一条支路都往报文钩子里抛帧，
// 界面上的通信报文面板就是靠这些调用活着的。
func (m *Master) transaction(slave byte, pdu []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryWait)
		}
		if !m.tx.IsOpen() {
			return nil, errors.New("连接已关闭")
		}
		if err := m.tx.Drain(); err != nil {
			return nil, err
		}

		req := m.fr.Build(slave, pdu)
		atomic.AddInt64(&m.txCount, 1)
		note := ""
		if attempt > 0 {
			note = fmt.Sprintf("第 %d 次重试", attempt+1)
		}
		m.emitFrame(FrameTx, req, note)
		if err := m.tx.Write(req); err != nil {
			lastErr = err
			m.emitFrame(FrameTx, req, "发送失败："+err.Error())
			continue
		}

		resp, err := m.readResponse(pdu)
		if err != nil {
			lastErr = err
			// 残帧也要记下来：从机回了半截就断了，是最典型的现场故障之一
			m.emitFrame(FrameRx, resp, err.Error())
			continue
		}
		atomic.AddInt64(&m.rxCount, 1)

		if msg := m.fr.Check(req, resp); msg != "" {
			lastErr = errors.New(msg)
			m.emitFrame(FrameRx, resp, msg)
			continue
		}
		if fc := m.fr.FuncCode(resp); fc&0x80 != 0 {
			code := byte(0)
			if body := m.fr.Body(resp); len(body) > 0 {
				code = body[0]
			}
			err = fmt.Errorf("从机返回异常 0x%02X：%s", fc, exceptionText(code))
			// 异常响应不重试，但必须记下来——这是从机在明说"你这条请求我不认"
			m.emitFrame(FrameRx, resp, err.Error())
			return nil, err
		}
		if got, want := m.fr.FuncCode(resp), pdu[0]; got != want {
			lastErr = fmt.Errorf("响应功能码不符（期望 0x%02X，收到 0x%02X）", want, got)
			m.emitFrame(FrameRx, resp, lastErr.Error())
			continue
		}
		m.emitFrame(FrameRx, resp, "")
		return m.fr.Body(resp), nil
	}
	return nil, lastErr
}

// ReadHoldingRegisters 功能码 03
func (m *Master) ReadHoldingRegisters(slave byte, start, count uint16) ([]uint16, error) {
	if count == 0 || count > MaxReadRegisters {
		return nil, fmt.Errorf("读取数量非法：%d", count)
	}
	pdu := []byte{
		models.FCReadHoldingRegisters,
		byte(start >> 8), byte(start),
		byte(count >> 8), byte(count),
	}
	body, err := m.transaction(slave, pdu)
	if err != nil {
		return nil, err
	}
	if len(body) < 1 {
		return nil, errors.New("响应缺少字节数")
	}
	if int(body[0]) != int(count)*2 {
		return nil, errors.New("响应字节数与请求数量不符")
	}
	if len(body) < 1+int(count)*2 {
		return nil, errors.New("响应数据长度不足")
	}
	regs := make([]uint16, count)
	for i := 0; i < int(count); i++ {
		regs[i] = uint16(body[1+2*i])<<8 | uint16(body[2+2*i])
	}
	return regs, nil
}

// WriteSingleRegister 功能码 06
func (m *Master) WriteSingleRegister(slave byte, addr, value uint16) error {
	pdu := []byte{
		models.FCWriteSingleRegister,
		byte(addr >> 8), byte(addr),
		byte(value >> 8), byte(value),
	}
	_, err := m.transaction(slave, pdu)
	return err
}

// WriteMultipleRegisters 功能码 16
func (m *Master) WriteMultipleRegisters(slave byte, addr uint16, regs []uint16) error {
	if len(regs) == 0 {
		return nil
	}
	if len(regs) > MaxWriteRegisters {
		return fmt.Errorf("一次最多写入 %d 个寄存器", MaxWriteRegisters)
	}
	pdu := []byte{
		models.FCWriteMultipleRegisters,
		byte(addr >> 8), byte(addr),
		byte(len(regs) >> 8), byte(len(regs)),
		byte(len(regs) * 2),
	}
	for _, r := range regs {
		pdu = append(pdu, byte(r>>8), byte(r))
	}
	_, err := m.transaction(slave, pdu)
	return err
}

// WriteTag 按点的功能码写入
func (m *Master) WriteTag(tag models.TagPoint, value any) error {
	switch tag.FunctionCode {
	case models.FCWriteSingleRegister:
		return m.WriteSingleRegister(tag.SlaveID, tag.Address, uint16(uint32(ToFloat(value))&0xFFFF))
	case models.FCWriteMultipleRegisters:
		return m.WriteMultipleRegisters(tag.SlaveID, tag.Address, EncodeRegisters(value, tag))
	default:
		return errors.New("当前点为只读（功能码 03），不可写入")
	}
}

// ReadTags 按块读取点表，返回与输入一一对应的值
func (m *Master) ReadTags(tags []models.TagPoint) []models.TagValue {
	if len(tags) == 0 {
		return nil
	}
	if !m.IsConnected() {
		out := make([]models.TagValue, 0, len(tags))
		for _, t := range tags {
			out = append(out, models.FailedValue(t.ID, "通道未连接"))
		}
		return out
	}

	out := make([]models.TagValue, 0, len(tags))
	for _, b := range SplitReadBlocks(tags) {
		regs, err := m.ReadHoldingRegisters(b.SlaveID, b.Start, b.Count)
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
