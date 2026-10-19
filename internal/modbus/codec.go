package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"modbus-hub-go/internal/models"
)

// RegisterCount 该数据类型占用的寄存器数（与 models.TagPoint.RegisterCount 一致，
// 这里再放一份是为了让编解码不依赖点位对象）
func RegisterCount(dt models.TagDataType) int {
	switch dt {
	case models.Float32, models.Int32:
		return 2
	case models.Double64:
		return 4
	default:
		return 1
	}
}

// applyByteOrder 把大端字节切片按字序规则重排，返回重排后的下标序列。
//
// 输入是数值的大端字节序列（byte[0] 为最高字节，记作 A），规则：
//   - ABCD 恒等：[A B C D]      → [AB][CD]
//   - CDAB 以字为单位反转：[C D A B] → [CD][AB]
//   - BADC 每个字内两字节交换：[B A D C] → [BA][DC]
//   - DCBA 全反转：[D C B A]    → [DC][BA]
//
// 同一套定义推广到 64 位就是现场熟知的 GHEF CDAB（CDAB）、AB CD EF GH（ABCD）、
// BA DC FE HG（BADC）、HG FE DC BA（DCBA），不需要为 64 位单写一份。
func applyByteOrder(src []byte, order models.ByteOrder) []byte {
	n := len(src)
	out := make([]byte, n)
	switch order {
	case models.ByteOrderABCD:
		copy(out, src)
	case models.ByteOrderCDAB:
		// 以 2 字节（一个寄存器）为一组，整组倒序排列
		for i := 0; i+1 < n; i += 2 {
			j := n - 2 - i
			out[i], out[i+1] = src[j], src[j+1]
		}
	case models.ByteOrderBADC:
		// 组内两字节对调，组间顺序不变
		for i := 0; i+1 < n; i += 2 {
			out[i], out[i+1] = src[i+1], src[i]
		}
	case models.ByteOrderDCBA:
		for i := 0; i < n; i++ {
			out[i] = src[n-1-i]
		}
	default:
		copy(out, src)
	}
	return out
}

// ParseRegisters 按点位的数据类型与字节顺序解析寄存器切片。
// 字节顺序只对 Float32 / Int32 / Double64 生效；UInt16 只有一个寄存器，直接取。
func ParseRegisters(regs []uint16, tag models.TagPoint) any {
	dt := tag.DataType
	if !tag.ByteOrder.Applies(dt) {
		if len(regs) == 0 {
			return uint16(0)
		}
		if dt == models.UInt16 {
			return regs[0]
		}
		return regs[0]
	}

	need := RegisterCount(dt)
	if len(regs) < need {
		return zeroValue(dt)
	}
	// 先按线序摊成字节（寄存器内高字节在前），再按字序规则重排回大端
	raw := make([]byte, need*2)
	for i := 0; i < need; i++ {
		raw[i*2] = byte(regs[i] >> 8)
		raw[i*2+1] = byte(regs[i])
	}
	be := applyByteOrder(raw, tag.ByteOrder)

	switch dt {
	case models.Int32:
		return int32(binary.BigEndian.Uint32(be))
	case models.Float32:
		return math.Float32frombits(binary.BigEndian.Uint32(be))
	case models.Double64:
		return math.Float64frombits(binary.BigEndian.Uint64(be))
	default:
		return binary.BigEndian.Uint16(be)
	}
}

func zeroValue(dt models.TagDataType) any {
	switch dt {
	case models.Int32:
		return int32(0)
	case models.Float32:
		return float32(0)
	case models.Double64:
		return float64(0)
	default:
		return uint16(0)
	}
}

// EncodeRegisters 把值编成寄存器（与 ParseRegisters 严格互逆）
func EncodeRegisters(value any, tag models.TagPoint) []uint16 {
	dt := tag.DataType
	need := RegisterCount(dt)

	var be []byte
	switch dt {
	case models.Int32:
		be = make([]byte, 4)
		binary.BigEndian.PutUint32(be, uint32(toInt32(value)))
	case models.Float32:
		be = make([]byte, 4)
		binary.BigEndian.PutUint32(be, math.Float32bits(float32(ToFloat(value))))
	case models.Double64:
		be = make([]byte, 8)
		binary.BigEndian.PutUint64(be, math.Float64bits(ToFloat(value)))
	default:
		be = make([]byte, 2)
		binary.BigEndian.PutUint16(be, toUint16(value))
	}

	if !tag.ByteOrder.Applies(dt) {
		// 单寄存器：无论什么字序都按线序直接给（与旧版一致）
		return []uint16{uint16(be[0])<<8 | uint16(be[1])}
	}

	// applyByteOrder 是自逆的 ABCD/CDAB 是对合、BADC 是对合、DCBA 是对合，
	// 所以编码用同一条规则把大端排回线序即可
	raw := applyByteOrder(be, tag.ByteOrder)
	out := make([]uint16, 0, need)
	for i := 0; i < need; i++ {
		out = append(out, uint16(raw[i*2])<<8|uint16(raw[i*2+1]))
	}
	return out
}

// ToFloat 把任意原始值转成 float64
func ToFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case uint16:
		return float64(t)
	case int64:
		return float64(t)
	case uint32:
		return float64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func toInt32(v any) int32 {
	switch t := v.(type) {
	case int32:
		return t
	default:
		return int32(ToFloat(v))
	}
}

func toUint16(v any) uint16 {
	switch t := v.(type) {
	case uint16:
		return t
	default:
		return uint16(uint32(ToFloat(v)) & 0xFFFF)
	}
}

// ParseInput 把界面输入的字符串（可带单位）解析成对应数据类型的值
func ParseInput(tag models.TagPoint, input string) (any, error) {
	s := StripUnit(strings.TrimSpace(input))
	switch tag.DataType {
	case models.UInt16:
		v, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return nil, errors.New("请输入 0~65535 的整数")
		}
		return uint16(v), nil
	case models.Int32:
		v, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return nil, errors.New("请输入合法的 32 位整数")
		}
		return int32(v), nil
	case models.Float32:
		v, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return nil, errors.New("请输入合法的单精度浮点数")
		}
		return float32(v), nil
	case models.Double64:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, errors.New("请输入合法的双精度浮点数")
		}
		return v, nil
	default:
		return s, nil
	}
}

// StripUnit 去掉数值后面带的单位，例如 "12.5 °C" → "12.5"
func StripUnit(input string) string {
	if _, err := strconv.ParseFloat(input, 64); err == nil {
		return input
	}
	if i := strings.LastIndex(input, " "); i > 0 {
		if _, err := strconv.ParseFloat(input[:i], 64); err == nil {
			return input[:i]
		}
	}
	return input
}

// BaseView 给出原始值的机器位模式：十六进制与二进制字符串。
// UInt16 为 16 位，Float32/Int32 为 32 位，Double64 为 64 位；
// 浮点数按 IEEE754 位模式展示（Float32 先转回 float32 再取位，避免 JSON 往返导致尾数变形）。
func BaseView(raw any, dt models.TagDataType) (hexText, binText string) {
	var bits uint64
	width := 16
	switch dt {
	case models.Int32:
		bits, width = uint64(uint32(toInt32(raw))), 32
	case models.Float32:
		bits, width = uint64(math.Float32bits(float32(ToFloat(raw)))), 32
	case models.Double64:
		bits, width = math.Float64bits(ToFloat(raw)), 64
	default:
		bits, width = uint64(toUint16(raw)), 16
	}
	return fmt.Sprintf("0x%0*X", width/4, bits), FormatBits(bits, width)
}

// FormatBits 按 4 位一组输出二进制，便于人工核对每一位的取值
func FormatBits(bits uint64, width int) string {
	groups := width / 4
	parts := make([]string, 0, groups)
	for i := groups - 1; i >= 0; i-- {
		parts = append(parts, fmt.Sprintf("%04b", (bits>>uint(i*4))&0xF))
	}
	return strings.Join(parts, " ")
}

// RegistersToHex 把报文里的原始寄存器按线序格式化成 "0x1234 0x5678"
func RegistersToHex(regs []uint16) string {
	if len(regs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(regs))
	for _, r := range regs {
		parts = append(parts, fmt.Sprintf("0x%04X", r))
	}
	return strings.Join(parts, " ")
}

// FormatValue 按点配置格式化显示值（含倍率、偏移与单位）
func FormatValue(raw any, tag models.TagPoint) string {
	scaled := ToFloat(raw)*tag.Scale + tag.Offset
	var number string
	if tag.DataType == models.Double64 {
		number = FormatScientific(scaled, tag.DecimalPlaces)
	} else {
		number = FormatNumber(scaled, tag.DecimalPlaces)
	}
	if strings.TrimSpace(tag.Unit) == "" {
		return number
	}
	return number + " " + tag.Unit
}

func FormatScientific(v float64, places int) string {
	return strconv.FormatFloat(v, 'e', clampPlaces(places), 64)
}

// FormatNumber 定点显示；当保留位导致显示为 0 时自动补位，避免小数值被抹平
func FormatNumber(v float64, places int) string {
	places = clampPlaces(places)
	s := strconv.FormatFloat(v, 'f', places, 64)
	if v == 0 {
		return s
	}
	if f, err := strconv.ParseFloat(s, 64); err != nil || f != 0 {
		return s
	}
	digits := int(math.Ceil(-math.Log10(math.Abs(v)))) + 1
	if digits < 1 {
		digits = 1
	}
	if d := max(places, digits); d > digits {
		digits = d
	}
	if digits > 12 {
		digits = 12
	}
	// 逐位补到能显示出非 0 为止（C# 原实现只补一次，小数仍可能被抹平）
	for {
		s = strconv.FormatFloat(v, 'f', digits, 64)
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f != 0 {
			return s
		}
		if digits >= 12 {
			break
		}
		digits++
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

func clampPlaces(p int) int {
	if p < 0 {
		return 0
	}
	if p > 12 {
		return 12
	}
	return p
}
