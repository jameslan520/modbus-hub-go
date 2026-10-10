package modbus

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"modbus-hub-go/internal/models"
)

// ParseRegisters 按数据类型解析寄存器切片。
// Float32 / Int32 为 CDAB（寄存器0=低字 CD，寄存器1=高字 AB）；
// Double64 为 GHEF CDAB（寄存器依次 GH、EF、CD、AB）。
func ParseRegisters(regs []uint16, dt models.TagDataType) any {
	switch dt {
	case models.UInt16:
		if len(regs) == 0 {
			return uint16(0)
		}
		return regs[0]
	case models.Int32:
		return RegistersToInt32Cdab(regs)
	case models.Float32:
		return RegistersToFloatCdab(regs)
	case models.Double64:
		return RegistersToDoubleGhefCdab(regs)
	default:
		if len(regs) == 0 {
			return uint16(0)
		}
		return regs[0]
	}
}

func RegistersToFloatCdab(regs []uint16) float32 {
	if len(regs) < 2 {
		return 0
	}
	bits := uint32(regs[0]) | uint32(regs[1])<<16
	return math.Float32frombits(bits)
}

func RegistersToInt32Cdab(regs []uint16) int32 {
	if len(regs) < 2 {
		return 0
	}
	bits := uint32(regs[0]) | uint32(regs[1])<<16
	return int32(bits)
}

func RegistersToDoubleGhefCdab(regs []uint16) float64 {
	if len(regs) < 4 {
		return 0
	}
	bits := uint64(regs[0]) | uint64(regs[1])<<16 | uint64(regs[2])<<32 | uint64(regs[3])<<48
	return math.Float64frombits(bits)
}

// EncodeRegisters 把值编成寄存器（与 ParseRegisters 严格互逆）
func EncodeRegisters(value any, dt models.TagDataType) []uint16 {
	switch dt {
	case models.Int32:
		return Int32ToRegistersCdab(toInt32(value))
	case models.Float32:
		return FloatToRegistersCdab(ToFloat(value))
	case models.Double64:
		return DoubleToRegistersGhefCdab(ToFloat(value))
	default:
		return []uint16{uint16(toUint16(value))}
	}
}

func FloatToRegistersCdab(v float64) []uint16 {
	bits := math.Float32bits(float32(v))
	return []uint16{uint16(bits & 0xFFFF), uint16(bits >> 16)}
}

func Int32ToRegistersCdab(v int32) []uint16 {
	bits := uint32(v)
	return []uint16{uint16(bits & 0xFFFF), uint16(bits >> 16)}
}

func DoubleToRegistersGhefCdab(v float64) []uint16 {
	bits := math.Float64bits(v)
	return []uint16{
		uint16(bits),
		uint16(bits >> 16),
		uint16(bits >> 32),
		uint16(bits >> 48),
	}
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
