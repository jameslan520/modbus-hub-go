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

// FrameInfo 一帧 RTU 报文的解析结果。
// 解析不出来也要有东西可看：至少给得出原始十六进制与一条说明，
// 现场对着串口调试助手核对时靠的就是这两样。
type FrameInfo struct {
	Slave     int          `json:"slave"`     // 从机地址，帧太短时为 -1
	FuncCode  int          `json:"funcCode"`  // 功能码（已去掉异常标志位），帧太短时为 -1
	Exception bool         `json:"exception"` // 是否为异常响应（功能码最高位为 1）
	ExCode    int          `json:"exCode"`    // 异常码，非异常响应时为 -1
	CRCOK     bool         `json:"crcOk"`     // CRC 校验是否通过（不含 CRC 的残帧为 false）
	Summary   string       `json:"summary"`   // 一句话摘要，如 "读保持寄存器 起始 0 数量 2"
	Fields    []FrameField `json:"fields"`    // 逐字段明细
}

// DescribeFrame 解析一帧 Modbus RTU 报文。
// data 为空或不足 4 字节（地址+功能码+CRC）时不会返回 error，
// 而是把能认出来的部分填进去、Summary 写明为什么认不全。
func DescribeFrame(data []byte, dir FrameDir) FrameInfo {
	info := FrameInfo{Slave: -1, FuncCode: -1, ExCode: -1}
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
