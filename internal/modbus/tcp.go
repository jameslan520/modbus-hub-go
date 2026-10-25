package modbus

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// drainQuiet TCP 排空等待窗口。
	// 上一帧读超时之后，残余字节还留在套接字缓冲里；这里用一个很短的读超时
	// 把当前积压的内容读完为止——读不到新数据就认为已经排空。
	drainQuiet = 50 * time.Millisecond
	// connectTimeout 拨号超时。现场网不通时要让用户在几秒内看到报错，
	// 而不是等系统默认的几十秒
	connectTimeout = 3 * time.Second
	// connWriteTimeout 单次写超时。一帧几十字节，正常毫秒级就发出去了
	connWriteTimeout = 3 * time.Second
)

// TCPTransport Modbus TCP 线路层：一条到设备监听端口的长连接。
//
// TCP 上不需要像串口那样靠静默间隔定帧，帧边界由 MBAP 头的长度字段给出，
// 所以这一层就是纯粹的"写满一帧、读满一帧"。
// 但要处理一件串口没有的麻烦：读超时之后残余字节会留在内核缓冲里，
// 下一次读取会从这个半截帧的腰部开始，整条流就此失同步——所以 Drain 必须真排空。
type TCPTransport struct {
	address string
	conn    net.Conn
	open    atomic.Bool
	connMu  sync.Mutex
	txCount int64
	rxCount int64
}

// NewTCPTransport 构造 TCP 线路层。host 可以是 IP 也可以是主机名，port 非法时补 502。
func NewTCPTransport(host string, port int) *TCPTransport {
	if port < 1 || port > 65535 {
		port = 502
	}
	return &TCPTransport{address: net.JoinHostPort(host, fmt.Sprint(port))}
}

// Open 拨号建立连接
func (t *TCPTransport) Open() error {
	conn, err := net.DialTimeout("tcp", t.address, connectTimeout)
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %w", t.address, err)
	}
	t.connMu.Lock()
	t.conn = conn
	t.connMu.Unlock()
	t.open.Store(true)
	return nil
}

func (t *TCPTransport) Close() error {
	t.open.Store(false)
	t.connMu.Lock()
	defer t.connMu.Unlock()
	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	return err
}

func (t *TCPTransport) IsOpen() bool { return t.open.Load() }

func (t *TCPTransport) Endpoint() string { return t.address }

func (t *TCPTransport) TxCount() int64 { return atomic.LoadInt64(&t.txCount) }
func (t *TCPTransport) RxCount() int64 { return atomic.LoadInt64(&t.rxCount) }

func (t *TCPTransport) Write(frame []byte) error {
	t.connMu.Lock()
	conn := t.conn
	t.connMu.Unlock()
	if conn == nil {
		return errors.New("TCP 连接未建立")
	}
	if err := conn.SetWriteDeadline(time.Now().Add(connWriteTimeout)); err != nil {
		return err
	}
	atomic.AddInt64(&t.txCount, 1)
	for sent := 0; sent < len(frame); {
		n, err := conn.Write(frame[sent:])
		if err != nil {
			// 写一般不会因为超时失败，失败基本都是链路断了，直接判死免得三次重试都在白等
			t.markDead()
			return fmt.Errorf("发送失败: %w", err)
		}
		sent += n
	}
	return nil
}

// Read 读满 n 字节。出错时返回已读到的部分，让调用方还能看到半截帧。
func (t *TCPTransport) Read(n int, timeout time.Duration) ([]byte, error) {
	t.connMu.Lock()
	conn := t.conn
	t.connMu.Unlock()
	if conn == nil {
		return nil, errors.New("TCP 连接未建立")
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	out := make([]byte, n)
	got := 0
	for got < n {
		k, err := conn.Read(out[got:])
		got += k
		if err != nil {
			if got == 0 {
				return nil, fmt.Errorf("读取失败: %w", err)
			}
			return out[:got], fmt.Errorf("响应超时（收到 %d/%d 字节）", got, n)
		}
	}
	atomic.AddInt64(&t.rxCount, 1)
	return out, nil
}

// Drain 把套接字里滞留的字节读干净。
// 这里故意不去区分"读到的是不是上一帧的尾巴"——Modbus TCP 是条字节流，
// 一旦错过帧边界就只能靠排空重新同步。
func (t *TCPTransport) Drain() error {
	if !t.open.Load() {
		return errors.New("TCP 连接未建立")
	}
	t.connMu.Lock()
	conn := t.conn
	t.connMu.Unlock()
	if conn == nil {
		return errors.New("TCP 连接未建立")
	}
	_ = conn.SetReadDeadline(time.Now().Add(drainQuiet))
	buf := make([]byte, 512)
	for {
		n, err := conn.Read(buf)
		if n == 0 || err != nil {
			// 超时（排干净了）或链路断了都算结束；具体进度交给下一次读取去暴露
			return nil
		}
	}
}

// markDead 链路彻底断了，标记掉免得继续重试
func (t *TCPTransport) markDead() {
	t.open.Store(false)
	t.connMu.Lock()
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
	t.connMu.Unlock()
}
