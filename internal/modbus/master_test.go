package modbus

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"modbus-hub-go/internal/models"
)

// shortRetry 把重试间隔压到毫秒级，用例不必真等
func shortRetry(t *testing.T) {
	t.Helper()
	old := retryWait
	retryWait = 2 * time.Millisecond
	t.Cleanup(func() { retryWait = old })
}

// scriptTransport 脚本化的线路层：把主站发出去的每一帧按原样记下来，
// 再按预先排好的队列把它想要的响应喂回去。
// 要造"读到一半断了"就把某一帧的字节数给少，要造彻底无响应就什么都不排。
type scriptTransport struct {
	mu       sync.Mutex
	open     bool
	sent     [][]byte
	replies  [][]byte
	endpoint string
}

func (s *scriptTransport) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open = true
	return nil
}

func (s *scriptTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open = false
	return nil
}

func (s *scriptTransport) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.open
}

func (s *scriptTransport) Endpoint() string {
	if s.endpoint == "" {
		return "SCRIPT"
	}
	return s.endpoint
}

func (s *scriptTransport) Write(frame []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, append([]byte{}, frame...))
	return nil
}

// Read 每次只给 n 个字节，剩下的留在队列里等下一次再来取——
// 这样两步读的分界点和真实链路一致，能验到 Master 的补齐逻辑
func (s *scriptTransport) Read(n int, timeout time.Duration) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.replies) == 0 {
		return nil, fmt.Errorf("响应超时（收到 0/%d 字节）", n)
	}
	out := make([]byte, 0, n)
	for len(out) < n && len(s.replies) > 0 {
		head := s.replies[0]
		take := n - len(out)
		if take > len(head) {
			take = len(head)
		}
		out = append(out, head[:take]...)
		if take == len(head) {
			s.replies = s.replies[1:]
		} else {
			s.replies[0] = head[take:]
		}
	}
	if len(out) < n {
		return out, fmt.Errorf("响应超时（收到 %d/%d 字节）", len(out), n)
	}
	return out, nil
}

func (s *scriptTransport) Drain() error {
	if !s.IsOpen() {
		return errors.New("连接已关闭")
	}
	return nil
}

func (s *scriptTransport) framesSent() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.sent))
	copy(out, s.sent)
	return out
}

// newScriptMaster 用脚本线路层拼一个主站，省掉真实串口
func newScriptMaster(fr Framing, replies ...[]byte) (*Master, *scriptTransport) {
	tx := &scriptTransport{replies: replies}
	return newMaster(tx, fr, 200*time.Millisecond), tx
}

func rtuReply(t *testing.T, body ...byte) []byte {
	t.Helper()
	return appendCRC(body)
}

// ---------- RTU ----------

func TestMasterRTUReadRegisters(t *testing.T) {
	m, tx := newScriptMaster(RTUFraming{},
		rtuReply(t, 0x01, 0x03, 0x04, 0x00, 0x12, 0x34, 0x56))
	if err := tx.Open(); err != nil {
		t.Fatal(err)
	}
	regs, err := m.ReadHoldingRegisters(1, 0, 2)
	if err != nil {
		t.Fatalf("读保持寄存器不应失败：%v", err)
	}
	if len(regs) != 2 || regs[0] != 0x0012 || regs[1] != 0x3456 {
		t.Fatalf("解析出的寄存器不对：%#v", regs)
	}
	want := rtuReply(t, 0x01, 0x03, 0x00, 0x00, 0x00, 0x02)
	if got := tx.framesSent(); len(got) != 1 || string(got[0]) != string(want) {
		t.Fatalf("发出的请求帧不对：%#v", got)
	}
}

// 从机返回异常响应只有 5 字节。以前按请求长度一次性读 9 字节，必然超时、
// 还要白等三个重试才能拿到"非法数据地址"；现在读到第 3 个字节就知道整帧多长。
func TestMasterRTUExceptionRepliesOnce(t *testing.T) {
	shortRetry(t)
	m, tx := newScriptMaster(RTUFraming{},
		rtuReply(t, 0x01, 0x83, 0x02))
	if err := tx.Open(); err != nil {
		t.Fatal(err)
	}

	var got []capturedFrame
	m.SetFrameHook(func(dir FrameDir, data []byte, note string) {
		got = append(got, capturedFrame{dir, FrameHex(data), note})
	})

	_, err := m.ReadHoldingRegisters(1, 0, 2)
	if err == nil || !strings.Contains(err.Error(), "非法数据地址") {
		t.Fatalf("应直接报出异常原因，实际：%v", err)
	}
	// 异常响应不重试：一来一回正好两帧
	if len(got) != 2 {
		t.Fatalf("异常响应不该重试，实际走了 %d 帧：%+v", len(got), got)
	}
	if got[0].dir != FrameTx || got[1].dir != FrameRx {
		t.Fatalf("帧序不对：%+v", got)
	}
	if !strings.Contains(got[1].note, "非法数据地址") {
		t.Fatalf("rx 帧的说明应写明异常原因，实际 %q", got[1].note)
	}
	if tx := len(tx.framesSent()); tx != 1 {
		t.Fatalf("只应发出去 1 帧，实际 %d", tx)
	}
}

// CRC 错了要重试到上限，每次重试都得在报文面板里留下痕迹
func TestMasterRTUCRCRetries(t *testing.T) {
	shortRetry(t)
	bad := []byte{0x01, 0x03, 0x04, 0x00, 0x12, 0x34, 0x56, 0x00, 0x00}
	m, tx := newScriptMaster(RTUFraming{}, bad, bad, bad)
	if err := tx.Open(); err != nil {
		t.Fatal(err)
	}

	var notes []string
	m.SetFrameHook(func(dir FrameDir, data []byte, note string) {
		notes = append(notes, string(dir)+":"+note)
	})

	if _, err := m.ReadHoldingRegisters(1, 0, 2); err == nil {
		t.Fatal("CRC 全错应最终失败")
	}
	if n := len(tx.framesSent()); n != maxRetries {
		t.Fatalf("应重试到上限 %d 次，实际发了 %d 帧", maxRetries, n)
	}
	for _, want := range []string{"tx:第 2 次重试", "tx:第 3 次重试", "rx:CRC 校验失败"} {
		if !strings.Contains(strings.Join(notes, "|"), want) {
			t.Fatalf("缺少 %q 的记录，实际：%v", want, notes)
		}
	}
}

// 从机回了半截就断，残帧必须进报文面板
func TestMasterRTUTruncatedReply(t *testing.T) {
	shortRetry(t)
	partialBytes := []byte{0x01, 0x03, 0x04}
	m, tx := newScriptMaster(RTUFraming{}, partialBytes)
	if err := tx.Open(); err != nil {
		t.Fatal(err)
	}
	var seen []capturedFrame
	m.SetFrameHook(func(dir FrameDir, data []byte, note string) {
		seen = append(seen, capturedFrame{dir, FrameHex(data), note})
	})
	if _, err := m.ReadHoldingRegisters(1, 0, 2); err == nil {
		t.Fatal("残帧应报错")
	}
	// 第一次尝试收到的就是那 3 个字节，后面的重试才是真的什么都没等到
	var partial *capturedFrame
	for i := range seen {
		if seen[i].dir == FrameRx && seen[i].hex != "" {
			partial = &seen[i]
			break
		}
	}
	if partial == nil {
		t.Fatalf("残帧应原样上报，实际：%+v", seen)
	}
	if partial.hex != "01 03 04" {
		t.Fatalf("残帧内容不对：%q", partial.hex)
	}
	if !strings.Contains(partial.note, "收到 3/9 字节") {
		t.Fatalf("说明应写明收到几分之几，实际 %q", partial.note)
	}
}

// ---------- Framing ----------

type capturedFrame struct {
	dir  FrameDir
	hex  string
	note string
}

func TestRTUFramingTotal(t *testing.T) {
	fr := RTUFraming{}
	// 读响应：3 字节头部 + 4 字节数据 + 2 字节 CRC
	if got := fr.Total([]byte{0x01, 0x03, 0x04}); got != 9 {
		t.Fatalf("03 响应应推算 9 字节，实际 %d", got)
	}
	if got := fr.Total([]byte{0x01, 0x83, 0x02}); got != 5 {
		t.Fatalf("异常响应应推算 5 字节，实际 %d", got)
	}
	if got := fr.Total([]byte{0x01, 0x06, 0x00}); got != 8 {
		t.Fatalf("06 响应应推算 8 字节，实际 %d", got)
	}
	if got := fr.Total([]byte{0x01, 0x10, 0x00}); got != 8 {
		t.Fatalf("16 响应应推算 8 字节，实际 %d", got)
	}
	// 未知功能码与不足下游长度的头部都推不出来
	if got := fr.Total([]byte{0x01, 0x7F, 0x00}); got != 0 {
		t.Fatalf("未知功能码应返回 0，实际 %d", got)
	}
	if got := fr.Total([]byte{0x01, 0x03}); got != 0 {
		t.Fatalf("头部不全应返回 0，实际 %d", got)
	}
}

func TestRTUFramingExpected(t *testing.T) {
	fr := RTUFraming{}
	if got := fr.Expected([]byte{0x03, 0x00, 0x00, 0x00, 0x02}); got != 9 {
		t.Fatalf("读 2 个寄存器应期望 9 字节，实际 %d", got)
	}
	if got := fr.Expected([]byte{0x06, 0x00, 0x10, 0x00, 0xFF}); got != 8 {
		t.Fatalf("写单个应期望 8 字节，实际 %d", got)
	}
	if got := fr.Expected(nil); got != 0 {
		t.Fatalf("空 PDU 应返回 0，实际 %d", got)
	}
}

func TestRTUFramingCheckAndBody(t *testing.T) {
	fr := RTUFraming{}
	req := fr.Build(1, []byte{0x03, 0x00, 0x00, 0x00, 0x02})
	if len(req) != 8 || req[0] != 1 {
		t.Fatalf("封帧不对：%#v", req)
	}
	if msg := fr.Check(req, rtuReply(t, 0x01, 0x03, 0x04, 0x00, 0x01)); msg != "" {
		t.Fatalf("正常帧不该报错：%s", msg)
	}
	if msg := fr.Check(req, rtuReply(t, 0x02, 0x03, 0x04, 0x00, 0x01)); !strings.Contains(msg, "从机地址不符") {
		t.Fatalf("从机不符应报错，实际 %q", msg)
	}
	if msg := fr.Check(req, []byte{0x01, 0x03, 0x00, 0x00}); !strings.Contains(msg, "CRC") {
		t.Fatalf("CRC 错应报错，实际 %q", msg)
	}
	if msg := fr.Check(req, []byte{0x01, 0x03}); !strings.Contains(msg, "帧长不足") {
		t.Fatalf("帧长不足应报错，实际 %q", msg)
	}
	// 数据域要剥掉首尾（从机地址 + CRC）并且跳过功能码：
	// 01 03 02 00 07 CRC CRC 的数据域就是 [02 00 07]，功能码不重复计入
	resp := rtuReply(t, 0x01, 0x03, 0x02, 0x00, 0x07)
	if got := fr.Body(resp); len(got) != 3 || got[0] != 0x02 {
		t.Fatalf("Body 应是功能码之后的数据域 [02 00 07]：%#v", got)
	}
	if fr.Slave(resp) != 1 || fr.FuncCode(resp) != 0x03 {
		t.Fatal("从机/功能码取错")
	}
	if fr.Body(nil) != nil || fr.Slave(nil) != 0 || fr.FuncCode(nil) != 0 {
		t.Fatal("空帧不能崩")
	}
}

func TestTCPFramingBuildIncrementsTxID(t *testing.T) {
	fr := &TCPFraming{}
	a := fr.Build(1, []byte{0x03, 0x00, 0x00, 0x00, 0x02})
	b := fr.Build(1, []byte{0x03, 0x00, 0x00, 0x00, 0x02})
	if len(a) != 12 {
		t.Fatalf("MBAP(7) + PDU(5) 应为 12 字节，实际 %d", len(a))
	}
	if a[2] != 0 || a[3] != 0 {
		t.Fatalf("协议标识必须为 0：%#v", a)
	}
	if int(beUint16(a, 4)) != len(a)-6 {
		t.Fatalf("长度字段应为其后字节数 %d，实际 %d", len(a)-6, beUint16(a, 4))
	}
	if a[6] != 1 {
		t.Fatalf("单元标识应是传入的从机号，实际 %d", a[6])
	}
	if beUint16(a, 0) == beUint16(b, 0) {
		t.Fatal("两次组帧的事务标识必须递增，否则分不清哪帧答哪帧")
	}
	if msg := fr.Check(b, a); !strings.Contains(msg, "事务标识不符") {
		t.Fatalf("应答错位必须报错，实际 %q", msg)
	}
}

func TestTCPFramingTotal(t *testing.T) {
	fr := &TCPFraming{}
	// MBAP 里长度字段标明后面还有 6 字节（单元标识 + PDU）
	head := []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x01}
	if got := fr.Total(head); got != 12 {
		t.Fatalf("整帧应为 12 字节，实际 %d", got)
	}
	if got := fr.Total(head[:5]); got != 0 {
		t.Fatalf("头部不全应返回 0，实际 %d", got)
	}
	// 长度字段离谱（标明 400 字节）时不能照着读
	bogus := []byte{0x00, 0x01, 0x00, 0x00, 0x01, 0x90, 0x01}
	if got := fr.Total(bogus); got != 0 {
		t.Fatalf("越界长度应返回 0，实际 %d", got)
	}
	// 长度字段小到连功能码都放不下，同样不算 Modbus
	tiny := []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x01}
	if got := fr.Total(tiny); got != 0 {
		t.Fatalf("短到不像话的帧应返回 0，实际 %d", got)
	}
}

func TestTCPFramingCheckAndBody(t *testing.T) {
	fr := &TCPFraming{}
	pdu := []byte{0x03, 0x00, 0x00, 0x00, 0x02}
	req := fr.Build(1, pdu)
	// 长度字段写的是"其后还有几字节"：单元标识 1 + 应答 PDU 6 = 7
	resp := append([]byte{req[0], req[1], 0x00, 0x00, 0x00, 0x07, 0x01}, 0x03, 0x04, 0x00, 0x12, 0x34, 0x56)
	if msg := fr.Check(req, resp); msg != "" {
		t.Fatalf("正常应答不该报错：%s", msg)
	}
	// 协议标识非 0：可能是别的协议用了同一个端口
	badPID := append([]byte{}, resp...)
	badPID[3] = 0x01
	if msg := fr.Check(req, badPID); !strings.Contains(msg, "协议标识") {
		t.Fatalf("协议标识错了要报错，实际 %q", msg)
	}
	// 长度字段与实际帧长对不上：这一帧是残的或是串了别的帧
	badLen := append([]byte{}, resp...)
	badLen[5] = 0x08
	if msg := fr.Check(req, badLen); !strings.Contains(msg, "长度字段") {
		t.Fatalf("长度不符要报错，实际 %q", msg)
	}
	if got := fr.Body(resp); string(got) != string([]byte{0x04, 0x00, 0x12, 0x34, 0x56}) {
		t.Fatalf("Body 应是剥掉 MBAP 与功能码后的数据域：%#v", got)
	}
	if fr.Slave(resp) != 1 || fr.FuncCode(resp) != 0x03 {
		t.Fatal("单元标识/功能码取错")
	}
	if fr.Body(nil) != nil || fr.Slave(nil) != 0 || fr.FuncCode(nil) != 0 {
		t.Fatal("空帧不能崩")
	}
	if got := fr.Check(req, resp[:4]); !strings.Contains(got, "帧长不足") {
		t.Fatalf("MBAP 都没读全要报错，实际 %q", got)
	}
}

// ---------- TCP 端到端 ----------

// serveLoop 处理一条连接上的所有问答，直到连接断开
func serveLoop(conn net.Conn, regs []uint16) {
	defer conn.Close()
	head := make([]byte, mbapHead)
	for {
		if _, err := io.ReadFull(conn, head); err != nil {
			return
		}
		total := 6 + int(beUint16(head, 4))
		if total < mbapHead+2 || total > mbapMax {
			return // 这一帧已经写坏了，接着读下去只会越读越偏
		}
		pdu := make([]byte, total-mbapHead)
		if _, err := io.ReadFull(conn, pdu); err != nil {
			return
		}
		rpdu := handlePDU(pdu, regs)
		out := make([]byte, 0, mbapHead+len(rpdu))
		out = append(out, head[0], head[1], 0x00, 0x00, byte((len(rpdu)+1)>>8), byte((len(rpdu) + 1)), head[6])
		out = append(out, rpdu...)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

// handlePDU 按功能码应答。地址越界回标准异常码 0x02（非法数据地址）
func handlePDU(pdu []byte, regs []uint16) []byte {
	exception := func(fc, code byte) []byte { return []byte{fc | 0x80, code} }
	switch pdu[0] {
	case models.FCReadHoldingRegisters:
		start := int(beUint16(pdu, 1))
		count := int(beUint16(pdu, 3))
		if start+count > len(regs) {
			return exception(pdu[0], 0x02)
		}
		out := []byte{pdu[0], byte(count * 2)}
		for i := 0; i < count; i++ {
			out = append(out, byte(regs[start+i]>>8), byte(regs[start+i]))
		}
		return out
	case models.FCWriteSingleRegister:
		addr := int(beUint16(pdu, 1))
		if addr >= len(regs) {
			return exception(pdu[0], 0x02)
		}
		regs[addr] = beUint16(pdu, 3)
		return append([]byte{}, pdu[:5]...)
	case models.FCWriteMultipleRegisters:
		addr := int(beUint16(pdu, 1))
		count := int(beUint16(pdu, 3))
		if addr+count > len(regs) {
			return exception(pdu[0], 0x02)
		}
		// 数据从 PDU 第 6 个字节开始：功能码 1 + 地址 2 + 数量 2 + 字节数 1
		for i := 0; i < count; i++ {
			regs[addr+i] = beUint16(pdu, 6+2*i)
		}
		return append([]byte{}, pdu[:5]...)
	default:
		return exception(pdu[0], 0x01) // 非法功能码
	}
}

// startServer 起服务端并在每条新连接上跑 serveLoop；返回 IP 与端口
func startServer(t *testing.T, regs []uint16) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起测试服务端失败：%v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveLoop(conn, regs)
		}
	}()
	h, p, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return h, n
}

func tcpMaster(t *testing.T, host string, port int) *Master {
	t.Helper()
	return NewMaster(models.ChannelConfig{
		ConnType:    models.ConnTCP,
		Host:        host,
		Port:        port,
		ReadTimeout: 1000,
	})
}

func TestMasterTCPReadWriteRoundTrip(t *testing.T) {
	regs := make([]uint16, 16)
	regs[2], regs[3] = 0x1234, 0x5678
	host, port := startServer(t, regs)

	m := tcpMaster(t, host, port)
	if err := m.Connect(); err != nil {
		t.Fatalf("连不上测试服务端：%v", err)
	}
	defer m.Close()
	if m.Protocol() != "TCP" {
		t.Fatalf("协议名应为 TCP，实际 %q", m.Protocol())
	}
	if !strings.HasPrefix(m.Endpoint(), "127.0.0.1:") {
		t.Fatalf("连接摘要不对：%q", m.Endpoint())
	}

	got, err := m.ReadHoldingRegisters(1, 2, 2)
	if err != nil {
		t.Fatalf("TCP 读保持寄存器失败：%v", err)
	}
	if len(got) != 2 || got[0] != 0x1234 || got[1] != 0x5678 {
		t.Fatalf("读回来的值不对：%#v", got)
	}

	if err := m.WriteSingleRegister(1, 5, 0x0102); err != nil {
		t.Fatalf("TCP 写单个寄存器失败：%v", err)
	}
	if regs[5] != 0x0102 {
		t.Fatalf("写入没有落进从机寄存器，实际 %#v", regs[5])
	}

	if err := m.WriteMultipleRegisters(1, 6, []uint16{0x0A, 0x0B, 0x0C}); err != nil {
		t.Fatalf("TCP 写多个寄存器失败：%v", err)
	}
	if regs[6] != 0x0A || regs[7] != 0x0B || regs[8] != 0x0C {
		t.Fatalf("批量写入不对：%#v", regs[6:9])
	}

	back, err := m.ReadHoldingRegisters(1, 5, 4)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if back[0] != 0x0102 || back[1] != 0x0A || back[3] != 0x0C {
		t.Fatalf("回读内容不对：%#v", back)
	}
	if m.TxCount() != 4 || m.RxCount() != 4 {
		t.Fatalf("收发计数不对：tx=%d rx=%d", m.TxCount(), m.RxCount())
	}
}

// TCP 上的异常响应同样要在 5 字节（这里是 MBAP+2）时就被认出来，不能拖到超时
func TestMasterTCPException(t *testing.T) {
	regs := make([]uint16, 4)
	host, port := startServer(t, regs)
	m := tcpMaster(t, host, port)
	if err := m.Connect(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	_, err := m.ReadHoldingRegisters(1, 2, 10) // 越过从机的寄存器范围
	if err == nil || !strings.Contains(err.Error(), "非法数据地址") {
		t.Fatalf("应收到的异常详情，实际：%v", err)
	}
}

// 事务标识是 TCP 上判断"这帧答的对不对"的唯一凭据，每次请求都要递增
func TestMasterTCPTransactionIDsIncrement(t *testing.T) {
	regs := make([]uint16, 8)
	host, port := startServer(t, regs)
	m := tcpMaster(t, host, port)
	if err := m.Connect(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	var txFrames, rxFrames []string
	m.SetFrameHook(func(dir FrameDir, data []byte, note string) {
		if dir == FrameTx {
			txFrames = append(txFrames, FrameHex(data))
		} else {
			rxFrames = append(rxFrames, FrameHex(data))
		}
	})
	if _, err := m.ReadHoldingRegisters(1, 0, 1); err != nil {
		t.Fatal(err)
	}
	if len(txFrames) != 1 || len(rxFrames) != 1 {
		t.Fatalf("正常读写应只有一来一回，实际 tx=%d rx=%d", len(txFrames), len(rxFrames))
	}
	txWords := strings.Fields(txFrames[0])
	rxWords := strings.Fields(rxFrames[0])
	if txWords[0] != rxWords[0] || txWords[1] != rxWords[1] {
		t.Fatalf("响应必须回显请求的事务标识：tx=%s rx=%s", txFrames[0], rxFrames[0])
	}
	if txWords[2] != "00" || txWords[3] != "00" {
		t.Fatalf("协议标识必须为 0000，实际 %s", txFrames[0])
	}
}

// 目标 IP 通不了时要给出看的明白的报错，而不是一句 dial tcp 的原始错误
func TestMasterTCPConnectFailure(t *testing.T) {
	m := NewMaster(models.ChannelConfig{
		ConnType:    models.ConnTCP,
		Host:        "127.0.0.1",
		Port:        1, // 端口 1 通常没人监听
		ReadTimeout: 200,
	})
	err := m.Connect()
	if err == nil {
		t.Fatal("连一个没监听的端口应该失败")
	}
	if !strings.Contains(err.Error(), "连接 ") || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("报错里应写明连的目标，实际：%v", err)
	}
}

// NewMaster 必须按通道类型挑对介质：串口给 RTU，网口给 TCP
func TestNewMasterPicksTransportByConnType(t *testing.T) {
	rtu := NewMaster(models.ChannelConfig{PortName: "COM3", BaudRate: 9600, ReadTimeout: 1000})
	if rtu.Protocol() != "RTU" || rtu.Endpoint() != "COM3" {
		t.Fatalf("串口通道应走 RTU，实际 %s @ %s", rtu.Protocol(), rtu.Endpoint())
	}
	tcp := NewMaster(models.ChannelConfig{ConnType: models.ConnTCP, Host: "10.0.0.5", Port: 502, ReadTimeout: 1000})
	if tcp.Protocol() != "TCP" || tcp.Endpoint() != "10.0.0.5:502" {
		t.Fatalf("网口通道应走 TCP，实际 %s @ %s", tcp.Protocol(), tcp.Endpoint())
	}
	// 端口忘填时底层要补默认端口，不能拼出 ":0" 这种地址
	dflt := NewMaster(models.ChannelConfig{ConnType: models.ConnTCP, Host: "10.0.0.5"})
	if dflt.Endpoint() != "10.0.0.5:502" {
		t.Fatalf("端口缺省应补 502，实际 %s", dflt.Endpoint())
	}
}

// 没连上就读点表，要给出"通道未连接"而不是空转
func TestMasterReadTagsWhenDisconnected(t *testing.T) {
	m := newMaster(&scriptTransport{}, RTUFraming{}, 50*time.Millisecond)
	tags := []models.TagPoint{{ID: "t1", Name: "压力", FunctionCode: models.FCReadHoldingRegisters, DataType: models.UInt16}}
	got := m.ReadTags(tags)
	if len(got) != 1 {
		t.Fatalf("应返回与点位一一对应的结果，实际 %d", len(got))
	}
	if got[0].Quality != "Bad" || !strings.Contains(got[0].ErrorMessage, "通道未连接") {
		t.Fatalf("每个点都应标成 Bad 并写明原因：%+v", got[0])
	}
}
