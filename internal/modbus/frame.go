package modbus

import (
	"fmt"
	"strings"
	"time"

	"modbus-hub-go/internal/models"
)

// FrameDir 报文方向
type FrameDir string

const (
	// FrameTx 主站发出去的请求帧
	FrameTx FrameDir = "tx"
	// FrameRx 从机回来的响应帧（含超时、CRC 错、异常响应这些"收到但不能用"的情况）
	FrameRx FrameDir = "rx"
)

// FrameField 一帧里某个字段的逐字节说明，界面展开详情时用
type FrameField struct {
	Name string `json:"name"` // 字段中文名，如 "从机地址"
	Hex  string `json:"hex"`  // 该字段的十六进制，如 "01"
	Text string `json:"text"` // 该字段的人话解释，如 "1"
}

// 帧格式标识：报文属于哪种链路，决定了这一帧按什么格式拆。
// 同一轮采集里串口与网口可能并存，这个标识由报文本身带来，不能靠猜。
const (
	ProtoRTU = "RTU"
	ProtoTCP = "TCP"
)

// DescribeProtocolFrame 按链路协议选解析器。
// 猜是猜不出来的：RTU 帧当 TCP 帧拆，字节看着也是那么回事，但每个字段都错位。
func DescribeProtocolFrame(protocol string, data []byte, dir FrameDir) FrameInfo {
	if strings.EqualFold(protocol, ProtoTCP) {
		return DescribeTCPFrame(data, dir)
	}
	return DescribeRTUFrame(data, dir)
}

// FrameInfo 一帧报文的解析结果。
// 解析不出来也要有东西可看：至少给得出原始十六进制与一条说明，
// 现场对着调试助手核对时靠的就是这两样。
type FrameInfo struct {
	// Protocol 链路协议：RTU / TCP。界面上用来说明这一帧是从哪条链路来的
	Protocol  string       `json:"protocol"`
	Slave     int          `json:"slave"`     // 从机地址（TCP 上是单元标识），帧太短时为 -1
	FuncCode  int          `json:"funcCode"`  // 功能码（已去掉异常标志位），帧太短时为 -1
	Exception bool         `json:"exception"` // 是否为异常响应（功能码最高位为 1）
	ExCode    int          `json:"exCode"`    // 异常码，非异常响应时为 -1
	CRCOK     bool         `json:"crcOk"`     // 帧级校验是否通过：RTU 看 CRC，TCP 看 MBAP 长度字段与协议标识
	Summary   string       `json:"summary"`   // 一句话摘要，如 "读保持寄存器 起始 0 数量 2"
	Fields    []FrameField `json:"fields"`    // 逐字段明细
}

// DescribeRTUFrame 解析一帧 Modbus RTU 报文。
// data 为空或不足 4 字节（地址+功能码+CRC）时不会返回 error，
// 而是把能认出来的部分填进去、Summary 写明为什么认不全。
func DescribeRTUFrame(data []byte, dir FrameDir) FrameInfo {
	info := FrameInfo{Protocol: ProtoRTU, Slave: -1, FuncCode: -1, ExCode: -1}
	if len(data) == 0 {
		info.Summary = "空帧"
		return info
	}
	if len(data) < 4 {
		info.Slave = int(data[0])
		if len(data) > 1 {
			info.FuncCode = int(data[1] & 0x7F)
			info.Exception = data[1]&0x80 != 0
		}
		info.Summary = fmt.Sprintf("残帧（只有 %d 字节，不足最小帧长 4）", len(data))
		info.Fields = []FrameField{
			{Name: "原始数据", Hex: hexJoin(data[:1]), Text: "从机地址"},
		}
		return info
	}

	info.Slave = int(data[0])
	info.FuncCode = int(data[1] & 0x7F)
	info.Exception = data[1]&0x80 != 0
	info.CRCOK = checkCRC(data)
	body := data[2 : len(data)-2]
	crc := data[len(data)-2:]

	info.Fields = append(info.Fields,
		FrameField{Name: "从机地址", Hex: hex1(data[0]), Text: fmt.Sprintf("%d", data[0])},
		FrameField{Name: "功能码", Hex: hex1(data[1]), Text: funcCodeText(data[1])},
	)

	if info.Exception {
		code := byte(0)
		if len(body) > 0 {
			code = body[0]
		}
		info.ExCode = int(code)
		info.Fields = append(info.Fields,
			FrameField{Name: "异常码", Hex: hex1(code), Text: exceptionText(code)},
		)
		info.Summary = fmt.Sprintf("异常响应：%s", exceptionText(code))
	} else {
		info.Fields = append(info.Fields, describeBody(info.FuncCode, body, dir)...)
		info.Summary = summarize(info.FuncCode, body, dir)
	}

	info.Fields = append(info.Fields, FrameField{
		Name: "CRC",
		Hex:  hexJoin(crc),
		Text: crcText(data),
	})
	return info
}

// describeBody 按功能码解析数据域。请求与响应的数据域布局完全不同，必须分开看。
func describeBody(fc int, body []byte, dir FrameDir) []FrameField {
	if dir == FrameTx {
		return describeRequest(fc, body)
	}
	return describeResponse(fc, body)
}

func describeRequest(fc int, body []byte) []FrameField {
	switch fc {
	case int(models.FCReadHoldingRegisters):
		if len(body) < 4 {
			return nil
		}
		start := beUint16(body, 0)
		count := beUint16(body, 2)
		return []FrameField{
			{Name: "起始地址", Hex: hexJoin(body[0:2]), Text: fmt.Sprintf("%d（0x%04X）", start, start)},
			{Name: "寄存器数量", Hex: hexJoin(body[2:4]), Text: fmt.Sprintf("%d", count)},
		}
	case int(models.FCWriteSingleRegister):
		if len(body) < 4 {
			return nil
		}
		addr := beUint16(body, 0)
		val := beUint16(body, 2)
		return []FrameField{
			{Name: "寄存器地址", Hex: hexJoin(body[0:2]), Text: fmt.Sprintf("%d（0x%04X）", addr, addr)},
			{Name: "写入值", Hex: hexJoin(body[2:4]), Text: fmt.Sprintf("%d（0x%04X）", val, val)},
		}
	case int(models.FCWriteMultipleRegisters):
		if len(body) < 5 {
			return nil
		}
		addr := beUint16(body, 0)
		count := beUint16(body, 2)
		vals := RegistersToHex(bytesToRegs(body[5:]))
		return []FrameField{
			{Name: "起始地址", Hex: hexJoin(body[0:2]), Text: fmt.Sprintf("%d（0x%04X）", addr, addr)},
			{Name: "寄存器数量", Hex: hexJoin(body[2:4]), Text: fmt.Sprintf("%d", count)},
			{Name: "字节数", Hex: hex1(body[4]), Text: fmt.Sprintf("%d", body[4])},
			{Name: "写入内容", Hex: hexJoin(body[5:]), Text: vals},
		}
	}
	if len(body) > 0 {
		return []FrameField{{Name: "数据域", Hex: hexJoin(body), Text: fmt.Sprintf("%d 字节", len(body))}}
	}
	return nil
}

func describeResponse(fc int, body []byte) []FrameField {
	switch fc {
	case int(models.FCReadHoldingRegisters):
		if len(body) < 1 {
			return nil
		}
		fields := []FrameField{
			{Name: "字节数", Hex: hex1(body[0]), Text: fmt.Sprintf("%d", body[0])},
		}
		if len(body) > 1 {
			fields = append(fields, FrameField{
				Name: "寄存器内容",
				Hex:  hexJoin(body[1:]),
				Text: RegistersToHex(bytesToRegs(body[1:])),
			})
		}
		return fields
	case int(models.FCWriteSingleRegister):
		if len(body) < 4 {
			return nil
		}
		addr := beUint16(body, 0)
		val := beUint16(body, 2)
		return []FrameField{
			{Name: "寄存器地址", Hex: hexJoin(body[0:2]), Text: fmt.Sprintf("%d（0x%04X）", addr, addr)},
			{Name: "写入值", Hex: hexJoin(body[2:4]), Text: fmt.Sprintf("%d（0x%04X）", val, val)},
		}
	case int(models.FCWriteMultipleRegisters):
		if len(body) < 4 {
			return nil
		}
		addr := beUint16(body, 0)
		count := beUint16(body, 2)
		return []FrameField{
			{Name: "起始地址", Hex: hexJoin(body[0:2]), Text: fmt.Sprintf("%d（0x%04X）", addr, addr)},
			{Name: "写入数量", Hex: hexJoin(body[2:4]), Text: fmt.Sprintf("%d", count)},
		}
	}
	if len(body) > 0 {
		return []FrameField{{Name: "数据域", Hex: hexJoin(body), Text: fmt.Sprintf("%d 字节", len(body))}}
	}
	return nil
}

func summarize(fc int, body []byte, dir FrameDir) string {
	fcName := funcCodeText(byte(fc))
	if dir == FrameTx {
		switch fc {
		case int(models.FCReadHoldingRegisters):
			if len(body) >= 4 {
				return fmt.Sprintf("%s 起始 %d 数量 %d", fcName, beUint16(body, 0), beUint16(body, 2))
			}
		case int(models.FCWriteSingleRegister):
			if len(body) >= 4 {
				return fmt.Sprintf("%s 地址 %d 值 %d", fcName, beUint16(body, 0), beUint16(body, 2))
			}
		case int(models.FCWriteMultipleRegisters):
			if len(body) >= 4 {
				return fmt.Sprintf("%s 起始 %d 数量 %d", fcName, beUint16(body, 0), beUint16(body, 2))
			}
		}
		return fcName + " 请求"
	}

	switch fc {
	case int(models.FCReadHoldingRegisters):
		if len(body) >= 1 {
			// 数据域第一个字节是"后面还有多少字节"，一个寄存器两个字节
			return fmt.Sprintf("%s 返回 %d 个寄存器", fcName, (len(body)-1)/2)
		}
	case int(models.FCWriteSingleRegister):
		if len(body) >= 4 {
			return fmt.Sprintf("%s 地址 %d 值 %d", fcName, beUint16(body, 0), beUint16(body, 2))
		}
	case int(models.FCWriteMultipleRegisters):
		if len(body) >= 4 {
			return fmt.Sprintf("%s 已写 %d 个寄存器", fcName, beUint16(body, 2))
		}
	}
	return fcName + " 响应"
}

// exceptionText 把异常码翻译成人话。
// 异常码是 Modbus 标准定死的那几种，从机靠它告诉我们这条请求为什么不认。
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

// funcCodeText 功能码中文名；异常响应（最高位为 1）单独标注
func funcCodeText(code byte) string {
	raw := code & 0x7F
	if code&0x80 != 0 {
		return fmt.Sprintf("0x%02X 异常响应（原功能码 0x%02X）", code, raw)
	}
	switch raw {
	case models.FCReadHoldingRegisters:
		return "03 读保持寄存器"
	case models.FCWriteSingleRegister:
		return "06 写单个寄存器"
	case models.FCWriteMultipleRegisters:
		return "16 写多个寄存器"
	default:
		return fmt.Sprintf("未知功能码 0x%02X", raw)
	}
}

// DescribeTCPFrame 解析一帧 Modbus TCP 报文。
//
// MBAP 头之后就是原封不动的 PDU，所以数据域与异常码的解析跟 RTU 完全共用一套
// （describeBody / summarize）；差的只是前面那 7 个字节怎么念。
func DescribeTCPFrame(data []byte, dir FrameDir) FrameInfo {
	info := FrameInfo{Protocol: ProtoTCP, Slave: -1, FuncCode: -1, ExCode: -1}
	if len(data) == 0 {
		info.Summary = "空帧"
		return info
	}
	if len(data) < mbapHead {
		info.Summary = fmt.Sprintf("残帧（只有 %d 字节，MBAP 头就要 %d）", len(data), mbapHead)
		if len(data) > 1 {
			// 事务标识是两字节，读了一半也能看出是第几帧在问，先顶上去
			info.Fields = []FrameField{{Name: "事务标识（残缺）", Hex: hexJoin(data[:len(data)-1]), Text: "帧还不完整"}}
		}
		return info
	}

	tid := beUint16(data, 0)
	pid := beUint16(data, 2)
	length := beUint16(data, 4)
	unit := data[6]
	pdu := data[mbapHead:]

	info.Slave = int(unit)
	// TCP 靠链路本身保证字节不出错，帧一级只有 MBAP 头可以做完整性核对：
	// 协议标识必须是 0，长度字段必须等于它后面实际有多少字节。
	info.CRCOK = pid == 0 && int(length) == len(data)-6
	if len(pdu) > 0 {
		info.FuncCode = int(pdu[0] & 0x7F)
		info.Exception = pdu[0]&0x80 != 0
	}

	info.Fields = []FrameField{
		{Name: "事务标识", Hex: hexJoin(data[0:2]), Text: fmt.Sprintf("%d（0x%04X）", tid, tid)},
		{Name: "协议标识", Hex: hexJoin(data[2:4]), Text: protocolIDText(pid)},
		{Name: "长度", Hex: hexJoin(data[4:6]), Text: tcpLengthText(length, len(data))},
		{Name: "单元标识", Hex: hex1(unit), Text: fmt.Sprintf("%d", unit)},
	}
	if len(pdu) == 0 {
		info.Summary = "MBAP 头之后没有 PDU"
		return info
	}
	info.Fields = append(info.Fields, FrameField{Name: "功能码", Hex: hex1(pdu[0]), Text: funcCodeText(pdu[0])})

	// 数据域从功能码之后开始，与 RTU 的数据域口径完全一致
	var body []byte
	if len(pdu) > 1 {
		body = pdu[1:]
	}
	if info.Exception {
		code := byte(0)
		if len(body) > 0 {
			code = body[0]
		}
		info.ExCode = int(code)
		info.Fields = append(info.Fields, FrameField{Name: "异常码", Hex: hex1(code), Text: exceptionText(code)})
		info.Summary = fmt.Sprintf("异常响应：%s", exceptionText(code))
		return info
	}
	info.Fields = append(info.Fields, describeBody(info.FuncCode, body, dir)...)
	info.Summary = summarize(info.FuncCode, body, dir)
	return info
}

func protocolIDText(pid uint16) string {
	if pid == 0 {
		return "0（Modbus）"
	}
	return fmt.Sprintf("0x%04X（不是 Modbus 协议）", pid)
}

func tcpLengthText(length uint16, frameLen int) string {
	if int(length) == frameLen-6 {
		return fmt.Sprintf("%d（其后 %d 字节，与实际一致）", length, frameLen-mbapHead)
	}
	return fmt.Sprintf("%d（其后应有 %d 字节，实际 %d，帧已错位）", length, length-1, frameLen-mbapHead)
}

// crcText CRC 校验结果的人话说明
func crcText(frame []byte) string {
	if checkCRC(frame) {
		return "校验通过"
	}
	want := crc16(frame[:len(frame)-2])
	got := uint16(frame[len(frame)-2]) | uint16(frame[len(frame)-1])<<8
	return fmt.Sprintf("校验失败（帧内 %04X，算出 %04X）", got, want)
}

// FrameHex 把一帧格式化成 "01 03 00 00 00 02 C4 0B"
func FrameHex(data []byte) string {
	return hexJoin(data)
}

// FrameASCII 可打印字符原样显示，其余用点占位；串口上跑的私有协议有时直接塞 ASCII
func FrameASCII(data []byte) string {
	var sb strings.Builder
	for _, b := range data {
		if b >= 0x20 && b < 0x7F {
			sb.WriteByte(b)
		} else {
			sb.WriteByte('.')
		}
	}
	return sb.String()
}

func hexJoin(data []byte) string {
	parts := make([]string, 0, len(data))
	for _, b := range data {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return strings.Join(parts, " ")
}

func hex1(b byte) string {
	return fmt.Sprintf("%02X", b)
}

func beUint16(b []byte, i int) uint16 {
	if i+1 >= len(b) {
		return 0
	}
	return uint16(b[i])<<8 | uint16(b[i+1])
}

// bytesToRegs 大端字节流转寄存器数组；多出来的单个字节按高位补零成一个寄存器
func bytesToRegs(b []byte) []uint16 {
	out := make([]uint16, 0, (len(b)+1)/2)
	for i := 0; i+1 < len(b); i += 2 {
		out = append(out, uint16(b[i])<<8|uint16(b[i+1]))
	}
	return out
}

// FrameStamp 帧时间戳文本：HH:MM:SS.mmm
func FrameStamp(t time.Time) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d",
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/int(time.Millisecond))
}
