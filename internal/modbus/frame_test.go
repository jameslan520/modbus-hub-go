package modbus

import (
	"strings"
	"testing"
	"time"
)

// 03 读保持寄存器：从机 1，起始 0，读 2 个（CRC 由 appendCRC 补）
func buildFrame(t *testing.T, body ...byte) []byte {
	t.Helper()
	return appendCRC(body)
}

func TestDescribeFrameReadRequest(t *testing.T) {
	req := buildFrame(t, 0x01, 0x03, 0x00, 0x00, 0x00, 0x02)
	info := DescribeRTUFrame(req, FrameTx)
	if info.Slave != 1 {
		t.Fatalf("从机地址应为 1，实际 %d", info.Slave)
	}
	if info.FuncCode != 3 {
		t.Fatalf("功能码应为 3，实际 %d", info.FuncCode)
	}
	if info.Exception {
		t.Fatal("正常请求不应判为异常响应")
	}
	if !info.CRCOK {
		t.Fatal("CRC 应校验通过")
	}
	if info.Summary != "03 读保持寄存器 起始 0 数量 2" {
		t.Fatalf("摘要不对：%q", info.Summary)
	}
	if len(info.Fields) == 0 {
		t.Fatal("应有逐字段明细")
	}
	// 字段顺序：从机 / 功能码 / 起始地址 / 数量 / CRC
	if info.Fields[0].Name != "从机地址" || info.Fields[1].Name != "功能码" {
		t.Fatalf("前两个字段应是从机与功能码，实际 %+v", info.Fields[:2])
	}
	if info.Fields[2].Name != "起始地址" || info.Fields[2].Text != "0（0x0000）" {
		t.Fatalf("起始地址字段不对：%+v", info.Fields[2])
	}
	if info.Fields[3].Name != "寄存器数量" || info.Fields[3].Text != "2" {
		t.Fatalf("数量字段不对：%+v", info.Fields[3])
	}
}

func TestDescribeFrameReadResponse(t *testing.T) {
	// 返回 2 个寄存器：0x0012 与 0x3456
	resp := buildFrame(t, 0x01, 0x03, 0x04, 0x00, 0x12, 0x34, 0x56)
	info := DescribeRTUFrame(resp, FrameRx)
	if info.Summary != "03 读保持寄存器 返回 2 个寄存器" {
		t.Fatalf("摘要不对：%q", info.Summary)
	}
	var found bool
	for _, f := range info.Fields {
		if f.Name == "寄存器内容" {
			found = true
			if f.Hex != "00 12 34 56" {
				t.Fatalf("寄存器内容十六进制不对：%q", f.Hex)
			}
			if f.Text != "0x0012 0x3456" {
				t.Fatalf("寄存器内容文本不对：%q", f.Text)
			}
		}
	}
	if !found {
		t.Fatal("响应应解析出寄存器内容")
	}
}

func TestDescribeFrameException(t *testing.T) {
	// 从机返回异常：功能码 0x83（03 + 0x80），异常码 0x02 非法数据地址
	resp := buildFrame(t, 0x01, 0x83, 0x02)
	info := DescribeRTUFrame(resp, FrameRx)
	if !info.Exception {
		t.Fatal("功能码最高位为 1 应判为异常响应")
	}
	if info.FuncCode != 3 {
		t.Fatalf("去掉异常位后功能码应为 3，实际 %d", info.FuncCode)
	}
	if info.ExCode != 2 {
		t.Fatalf("异常码应为 2，实际 %d", info.ExCode)
	}
	if !strings.Contains(info.Summary, "非法数据地址") {
		t.Fatalf("摘要应写明异常原因，实际 %q", info.Summary)
	}
}

func TestDescribeFrameWriteSingle(t *testing.T) {
	req := buildFrame(t, 0x02, 0x06, 0x00, 0x10, 0x00, 0xFF)
	info := DescribeRTUFrame(req, FrameTx)
	if info.Slave != 2 || info.FuncCode != 6 {
		t.Fatalf("从机/功能码不对：%d / %d", info.Slave, info.FuncCode)
	}
	if info.Summary != "06 写单个寄存器 地址 16 值 255" {
		t.Fatalf("摘要不对：%q", info.Summary)
	}
}

func TestDescribeFrameWriteMultiple(t *testing.T) {
	// 从机 1，起始 4，写 2 个，4 字节：0x0001 0x0002
	req := buildFrame(t, 0x01, 0x10, 0x00, 0x04, 0x00, 0x02, 0x04, 0x00, 0x01, 0x00, 0x02)
	info := DescribeRTUFrame(req, FrameTx)
	if info.FuncCode != 16 {
		t.Fatalf("功能码应为 16，实际 %d", info.FuncCode)
	}
	if info.Summary != "16 写多个寄存器 起始 4 数量 2" {
		t.Fatalf("摘要不对：%q", info.Summary)
	}
	for _, f := range info.Fields {
		if f.Name == "写入内容" && f.Text != "0x0001 0x0002" {
			t.Fatalf("写入内容不对：%q", f.Text)
		}
		if f.Name == "字节数" && f.Text != "4" {
			t.Fatalf("字节数不对：%q", f.Text)
		}
	}

	// 响应只有起始地址与数量
	resp := buildFrame(t, 0x01, 0x10, 0x00, 0x04, 0x00, 0x02)
	rinfo := DescribeRTUFrame(resp, FrameRx)
	if rinfo.Summary != "16 写多个寄存器 已写 2 个寄存器" {
		t.Fatalf("响应摘要不对：%q", rinfo.Summary)
	}
}

// CRC 错的帧也要能认出从机与功能码，并在说明里给出"帧内/算出"两个值，
// 现场就是靠这个判断是干扰还是从机算错
func TestDescribeFrameCRCFail(t *testing.T) {
	frame := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00}
	info := DescribeRTUFrame(frame, FrameRx)
	if info.CRCOK {
		t.Fatal("CRC 应校验失败")
	}
	if info.Slave != 1 || info.FuncCode != 3 {
		t.Fatalf("CRC 错了也要认出从机与功能码：%d / %d", info.Slave, info.FuncCode)
	}
	var crcField FrameField
	for _, f := range info.Fields {
		if f.Name == "CRC" {
			crcField = f
		}
	}
	if !strings.Contains(crcField.Text, "校验失败") {
		t.Fatalf("CRC 字段应写明校验失败，实际 %q", crcField.Text)
	}
}

// 残帧（超时只收到几个字节）不能让界面崩掉，要给出原因
func TestDescribeFrameTruncated(t *testing.T) {
	info := DescribeRTUFrame([]byte{0x01, 0x03, 0x00}, FrameRx)
	if info.Slave != 1 {
		t.Fatalf("残帧也要认出从机，实际 %d", info.Slave)
	}
	if !strings.Contains(info.Summary, "残帧") {
		t.Fatalf("应说明是残帧，实际 %q", info.Summary)
	}
	if info.CRCOK {
		t.Fatal("残帧的 CRC 不能算通过")
	}

	empty := DescribeRTUFrame(nil, FrameRx)
	if empty.Summary != "空帧" || empty.Slave != -1 {
		t.Fatalf("空帧处理不对：%+v", empty)
	}
}

func TestFrameHexAndASCII(t *testing.T) {
	frame := []byte{0x01, 0x03, 0x00, 0x00}
	if got := FrameHex(frame); got != "01 03 00 00" {
		t.Fatalf("十六进制文本不对：%q", got)
	}
	// 可打印字符原样输出，控制字符用点占位
	if got := FrameASCII([]byte("AB\x01\x00")); got != "AB.." {
		t.Fatalf("ASCII 文本不对：%q", got)
	}
}

// ---------- Modbus TCP 的 MBAP 帧 ----------

// tcpFrame 拼一个完整的 TCP 帧：MBAP 头 + PDU，长度字段按实际算
func tcpFrame(t *testing.T, txid uint16, unit byte, pdu ...byte) []byte {
	t.Helper()
	length := len(pdu) + 1
	out := []byte{byte(txid >> 8), byte(txid), 0x00, 0x00, byte(length >> 8), byte(length), unit}
	return append(out, pdu...)
}

func TestDescribeTCPFrameReadRequest(t *testing.T) {
	frame := tcpFrame(t, 0x0007, 1, 0x03, 0x00, 0x00, 0x00, 0x02)
	info := DescribeTCPFrame(frame, FrameTx)
	if info.Protocol != ProtoTCP {
		t.Fatalf("协议标识应为 TCP，实际 %q", info.Protocol)
	}
	if info.Slave != 1 || info.FuncCode != 3 {
		t.Fatalf("单元标识/功能码不对：%+v", info)
	}
	if !info.CRCOK {
		t.Fatal("长度字段与实际一致应判为校验通过")
	}
	if info.Summary != "03 读保持寄存器 起始 0 数量 2" {
		t.Fatalf("摘要应与 RTU 完全一致，实际 %q", info.Summary)
	}
	want := []string{"事务标识", "协议标识", "长度", "单元标识", "功能码", "起始地址", "寄存器数量"}
	if len(info.Fields) != len(want) {
		t.Fatalf("字段数量不对，实际 %+v", info.Fields)
	}
	for i, name := range want {
		if info.Fields[i].Name != name {
			t.Fatalf("第 %d 个字段应是 %s，实际 %s", i, name, info.Fields[i].Name)
		}
	}
	if info.Fields[0].Text != "7（0x0007）" {
		t.Fatalf("事务标识要给出十进制与十六进制两种：%q", info.Fields[0].Text)
	}
	if info.Fields[1].Text != "0（Modbus）" {
		t.Fatalf("协议标识要写明是 Modbus：%q", info.Fields[1].Text)
	}
	if info.Fields[3].Text != "1" {
		t.Fatalf("单元标识不对：%q", info.Fields[3].Text)
	}
}

func TestDescribeTCPFrameReadResponse(t *testing.T) {
	frame := tcpFrame(t, 0x0008, 1, 0x03, 0x04, 0x00, 0x12, 0x34, 0x56)
	info := DescribeTCPFrame(frame, FrameRx)
	if info.Summary != "03 读保持寄存器 返回 2 个寄存器" {
		t.Fatalf("摘要不对：%q", info.Summary)
	}
	var found bool
	for _, f := range info.Fields {
		if f.Name == "寄存器内容" && f.Text == "0x0012 0x3456" {
			found = true
		}
	}
	if !found {
		t.Fatal("响应应解出寄存器内容")
	}
}

func TestDescribeTCPFrameException(t *testing.T) {
	frame := tcpFrame(t, 0x0009, 2, 0x83, 0x02)
	info := DescribeTCPFrame(frame, FrameRx)
	if !info.Exception || info.FuncCode != 3 || info.ExCode != 2 {
		t.Fatalf("异常响应解析不对：%+v", info)
	}
	if !strings.Contains(info.Summary, "非法数据地址") {
		t.Fatalf("摘要应写明异常原因，实际 %q", info.Summary)
	}
}

// 长度字段跟实际帧长对不上，说明这一帧已经错位：要明确报错且不能算校验通过
func TestDescribeTCPFrameLengthMismatch(t *testing.T) {
	frame := tcpFrame(t, 0x000A, 1, 0x03, 0x04, 0x00, 0x12, 0x34, 0x56)
	frame[5] = 0x09 // 原本是 0x07
	info := DescribeTCPFrame(frame, FrameRx)
	if info.CRCOK {
		t.Fatal("长度字段与实际不符不能算校验通过")
	}
	var field FrameField
	for _, f := range info.Fields {
		if f.Name == "长度" {
			field = f
		}
	}
	if !strings.Contains(field.Text, "帧已错位") {
		t.Fatalf("长度字段要说清楚错在哪，实际 %q", field.Text)
	}
}

// 协议标识非 0 意味着这个端口上跑的不是 Modbus
func TestDescribeTCPFrameBadProtocolID(t *testing.T) {
	frame := tcpFrame(t, 0x000B, 1, 0x03, 0x00, 0x00, 0x00, 0x02)
	frame[3] = 0x01
	info := DescribeTCPFrame(frame, FrameTx)
	if info.CRCOK {
		t.Fatal("协议标识不为 0 不能算校验通过")
	}
	if !strings.Contains(info.Fields[1].Text, "不是 Modbus") {
		t.Fatalf("应说明不是 Modbus，实际 %q", info.Fields[1].Text)
	}
}

func TestDescribeTCPFrameTruncated(t *testing.T) {
	info := DescribeTCPFrame([]byte{0x00, 0x01, 0x00}, FrameRx)
	if !strings.Contains(info.Summary, "残帧") {
		t.Fatalf("MBAP 没读全应说明是残帧，实际 %q", info.Summary)
	}
	if info.Slave != -1 || info.FuncCode != -1 {
		t.Fatalf("残帧不该编出从机与功能码：%+v", info)
	}
	// MBAP 头齐了但 PDU 是空的
	empty := DescribeTCPFrame([]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x01}, FrameTx)
	if !strings.Contains(empty.Summary, "没有 PDU") {
		t.Fatalf("只有 MBAP 头要说清楚，实际 %q", empty.Summary)
	}
	if empty := DescribeTCPFrame(nil, FrameRx); empty.Summary != "空帧" {
		t.Fatalf("空帧处理不对：%+v", empty)
	}
}

// 短到连 MBAP 头都没有的帧不能让它 panic，各分支都要安全返回
func TestDescribeTCPFrameShortAndUnknown(t *testing.T) {
	cases := [][]byte{
		{0x00, 0x01},
		{0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x01},                   // 只有 MBAP 头
		{0x00, 0x01, 0x00, 0x00, 0x00, 0x05, 0x01, 0x7F, 0x00},       // 未知功能码
		{0x00, 0x01, 0x00, 0x00, 0x00, 0x03, 0x01, 0x10, 0x00, 0x04}, // 16 的数据域不足
	}
	for _, c := range cases {
		for _, dir := range []FrameDir{FrameTx, FrameRx} {
			info := DescribeTCPFrame(c, dir)
			if info.Summary == "" {
				t.Fatalf("%#v (%s) 应给出摘要，不能为空", c, dir)
			}
		}
	}
}

// RTU 帧按 TCP 拆会全错，所以走哪个解析器必须由 Field 明确指定
func TestDescribeProtocolFrameDispatch(t *testing.T) {
	rtu := buildFrame(t, 0x01, 0x03, 0x00, 0x00, 0x00, 0x02)
	if got := DescribeProtocolFrame(ProtoRTU, rtu, FrameTx); got.Protocol != ProtoRTU || got.Slave != 1 {
		t.Fatalf("指定 RTU 就该按 RTU 拆：%+v", got)
	}
	if got := DescribeProtocolFrame(ProtoTCP, rtu, FrameTx); got.Protocol != ProtoTCP {
		t.Fatalf("指定 TCP 就该按 TCP 拆：%+v", got)
	}
	// 空串与未知值按 RTU 处理：老报文没有这个字段时不能解析不出来
	if got := DescribeProtocolFrame("", rtu, FrameTx); got.Protocol != ProtoRTU {
		t.Fatalf("未标明协议的应按 RTU 处理，实际 %q", got.Protocol)
	}
}

func TestFrameStamp(t *testing.T) {
	ts := time.Date(2026, 10, 25, 9, 5, 3, 123000000, time.Local)
	if got := FrameStamp(ts); got != "09:05:03.123" {
		t.Fatalf("时间戳格式不对：%q", got)
	}
}

// 数据域被截断时不能越界，各分支都要安全返回
func TestDescribeFrameShortBody(t *testing.T) {
	cases := [][]byte{
		{0x01, 0x03, 0x00},       // 03 请求数据域不足 4 字节
		{0x01, 0x06, 0x00},       // 06 请求数据域不足
		{0x01, 0x10, 0x00, 0x04}, // 16 请求数据域不足 5 字节
		{0x01, 0x03, 0x04},       // 03 响应只有字节数没有内容
		{0x01, 0x7F, 0x00, 0x00}, // 未知功能码
	}
	for _, c := range cases {
		for _, dir := range []FrameDir{FrameTx, FrameRx} {
			info := DescribeRTUFrame(appendCRC(c), dir)
			if info.Summary == "" {
				t.Fatalf("%#v (%s) 应给出摘要，不能为空", c, dir)
			}
		}
	}
}

// ---------- 报文钩子 ----------

// 钩子在主站上只是个回调，不接链路也能验：直接调 emitFrame 看它是否原样上报。
// 顺带确认上报的是副本——底层切片被后续读写复用时，外部不能跟着变。
func TestSetFrameHookReportsCopy(t *testing.T) {
	c := &Master{}
	var gotDir FrameDir
	var gotData []byte
	var gotNote string
	c.SetFrameHook(func(dir FrameDir, data []byte, note string) {
		gotDir, gotData, gotNote = dir, data, note
	})

	frame := []byte{0x01, 0x03, 0x00, 0x00}
	c.emitFrame(FrameTx, frame, "第 2 次重试")
	if gotDir != FrameTx {
		t.Fatalf("方向不对：%q", gotDir)
	}
	if string(gotData) != string(frame) {
		t.Fatalf("报文内容不对：%#v", gotData)
	}
	if gotNote != "第 2 次重试" {
		t.Fatalf("说明不对：%q", gotNote)
	}

	// 改掉原始切片，钩子已经拿到的副本不能跟着变
	frame[0] = 0xFF
	if gotData[0] != 0x01 {
		t.Fatalf("上报的应是副本，实际被改成了 %#v", gotData)
	}
}

// 没设钩子时调用不能崩——直连场景与老用例都不挂钩子
func TestEmitFrameWithoutHook(t *testing.T) {
	c := &Master{}
	c.emitFrame(FrameRx, []byte{0x01, 0x03}, "")
}

// 读取超时必须把已收到的残帧交出来，这是排查"从机回了半截就断"的唯一线索
func TestReadExactReturnsPartialOnTimeout(t *testing.T) {
	c := &RTUTransport{
		rxCh:   make(chan byte, 16),
		stopCh: make(chan struct{}),
	}
	c.rxCh <- 0x01
	c.rxCh <- 0x03
	c.rxCh <- 0x02

	got, err := c.Read(8, 20*time.Millisecond)
	if err == nil {
		t.Fatal("读不满应报错")
	}
	if len(got) != 3 {
		t.Fatalf("应把已收到的 3 个字节交出来，实际 %d：%#v", len(got), got)
	}
	if !strings.Contains(err.Error(), "3/8") {
		t.Fatalf("错误信息应写明收到/期望字节数，实际 %q", err.Error())
	}
}

// 读满了就正常返回，不走超时分支
func TestReadExactComplete(t *testing.T) {
	c := &RTUTransport{
		rxCh:   make(chan byte, 16),
		stopCh: make(chan struct{}),
	}
	want := []byte{0x01, 0x03, 0x00, 0x00}
	for _, b := range want {
		c.rxCh <- b
	}
	got, err := c.Read(4, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("内容不对：%#v", got)
	}
}
