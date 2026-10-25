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

// RTUTransport 串口线路层：只管把整帧字节从 RS485 总线上收回来、发出去。
// Modbus 的语义都不在这一层，前缀从机地址与后缀 CRC 由 RTUFraming 加。
type RTUTransport struct {
	portName string
	mode     *serial.Mode

	port     serial.Port
	rxCh     chan byte
	stopCh   chan struct{}
	open     atomic.Bool
	closeMu  sync.Mutex
	txCount  int64
	rxCount  int64
}

// NewRTUTransport 按通道配置构造串口线路层
func NewRTUTransport(cfg models.ChannelConfig) *RTUTransport {
	dataBits := cfg.DataBits
	if dataBits != 7 && dataBits != 8 {
		dataBits = 8
	}
	return &RTUTransport{
		portName: cfg.PortName,
		mode: &serial.Mode{
			BaudRate: cfg.BaudRate,
			DataBits: dataBits,
			Parity:   toSerialParity(cfg.Parity),
			StopBits: toSerialStopBits(cfg.StopBits),
		},
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

// Open 打开串口并启动接收协程
func (t *RTUTransport) Open() error {
	port, err := serial.Open(t.portName, t.mode)
	if err != nil {
		return fmt.Errorf("打开串口 %s 失败: %w", t.portName, err)
	}
	t.port = port
	t.rxCh = make(chan byte, 8192)
	t.stopCh = make(chan struct{})
	t.open.Store(true)
	go t.readLoop()
	return nil
}

// Close 关闭串口
func (t *RTUTransport) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	t.open.Store(false)
	if t.stopCh != nil {
		select {
		case <-t.stopCh:
			// 已经关过了
		default:
			close(t.stopCh)
		}
	}
	if t.port != nil {
		return t.port.Close()
	}
	return nil
}

func (t *RTUTransport) IsOpen() bool { return t.open.Load() }

func (t *RTUTransport) Endpoint() string { return t.portName }

func (t *RTUTransport) TxCount() int64 { return atomic.LoadInt64(&t.txCount) }
func (t *RTUTransport) RxCount() int64 { return atomic.LoadInt64(&t.rxCount) }

func (t *RTUTransport) Write(frame []byte) error {
	if t.port == nil {
		return errors.New("串口未打开")
	}
	atomic.AddInt64(&t.txCount, 1)
	if _, err := t.port.Write(frame); err != nil {
		return fmt.Errorf("写串口 %s 失败: %w", t.portName, err)
	}
	return nil
}

// Read 读满 n 字节。超时或串口关闭时把已收到的部分一并返回。
func (t *RTUTransport) Read(n int, timeout time.Duration) ([]byte, error) {
	out := make([]byte, 0, n)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(out) < n {
		select {
		case b := <-t.rxCh:
			out = append(out, b)
		case <-timer.C:
			return out, fmt.Errorf("响应超时（收到 %d/%d 字节）", len(out), n)
		case <-t.stopCh:
			return out, errors.New("串口已关闭")
		}
	}
	return out, nil
}

// Drain 清空接收缓冲
func (t *RTUTransport) Drain() error {
	if !t.open.Load() {
		return errors.New("串口已关闭")
	}
	for {
		select {
		case <-t.rxCh:
		case <-t.stopCh:
			return errors.New("串口已关闭")
		default:
			return nil
		}
	}
}

func (t *RTUTransport) readLoop() {
	buf := make([]byte, 256)
	for {
		n, err := t.port.Read(buf)
		for i := 0; i < n; i++ {
			select {
			case t.rxCh <- buf[i]:
			case <-t.stopCh:
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
