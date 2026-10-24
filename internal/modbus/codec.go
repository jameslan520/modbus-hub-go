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

// ParseWriteInput 按输入口径把界面输入解析成写进寄存器的原始值。
//
// raw 原始值：直接按数据类型解析十进制（可带单位后缀）。
// hex 十六进制：按 IEEE754 位模式解析，跟点表"十六进制"列显示的是同一套东西，
//
//	因此填的是"值的高字节在前"的正常顺序，下发时再按本点的字节顺序拆成寄存器。
//
// eng 工程值：现场读数，按 raw = (eng − offset) ÷ scale 反算。
func ParseWriteInput(tag models.TagPoint, input string, mode models.InputMode) (any, error) {
	switch mode {
	case models.InputModeHex:
		return parseHexBits(tag.DataType, input)
	case models.InputModeEng:
		return parseEngineering(tag, input)
	default:
		return ParseInput(tag, input)
	}
}

// parseHexBits 解析十六进制位模式。
// 容忍 0x 前缀、h/H 后缀、空格与下划线分组，大小写不限；
// 位数不足时按高位补零（写 "FDB" 等同 "00000FDB"）。
func parseHexBits(dt models.TagDataType, input string) (any, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	s = strings.NewReplacer("0x", "", " ", "", "_", "", ",", "").Replace(s)
	s = strings.TrimSuffix(s, "h")
	if s == "" {
		return nil, errors.New("请输入十六进制数值")
	}
	for _, c := range s {
		isDigit := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isDigit {
			return nil, fmt.Errorf("包含非十六进制字符：%q", string(c))
		}
	}
	want := RegisterCount(dt) * 4 // 一个寄存器 4 个十六进制字符
	if len(s) > want {
		return nil, fmt.Errorf("%s 最多 %d 位十六进制（%d 个寄存器），当前 %d 位",
			dataTypeText(dt), want, RegisterCount(dt), len(s))
	}
	s = strings.Repeat("0", want-len(s)) + s
	bits, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return nil, fmt.Errorf("十六进制解析失败：%w", err)
	}
	switch dt {
	case models.UInt16:
		return uint16(bits), nil
	case models.Int32:
		return int32(uint32(bits)), nil
	case models.Float32:
		return math.Float32frombits(uint32(bits)), nil
	case models.Double64:
		return math.Float64frombits(bits), nil
	default:
		return uint16(bits), nil
	}
}

// parseEngineering 由工程值反算原始值：raw = (eng − offset) ÷ scale。
// 反算结果可能落不到寄存器能表示的精度上（整数类型有小数、float32 有截断），
// 这里统一就近取整/收敛，并由调用方通过预览告诉用户实际下发的是什么。
func parseEngineering(tag models.TagPoint, input string) (any, error) {
	raw, err := engineeringRaw(tag, input)
	if err != nil {
		return nil, err
	}
	return coerceRaw(tag, raw)
}

// engineeringRaw 只做反算，不做类型收敛，便于预览对比"理论值"与"实际下发值"
func engineeringRaw(tag models.TagPoint, input string) (float64, error) {
	s := StripUnit(strings.TrimSpace(input))
	if s == "" {
		return 0, errors.New("请输入工程值")
	}
	eng, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, errors.New("请输入合法的工程值数字")
	}
	if tag.Scale == 0 {
		return 0, errors.New("该点位倍率为 0，无法由工程值反算原始值")
	}
	return (eng - tag.Offset) / tag.Scale, nil
}

// coerceRaw 把反算出来的理论值收敛到该数据类型真正能表示的值
func coerceRaw(tag models.TagPoint, raw float64) (any, error) {
	switch tag.DataType {
	case models.UInt16:
		r := math.Round(raw)
		if r < 0 || r > 65535 {
			return nil, fmt.Errorf("反算出的原始值 %.4f 超出 0~65535，无法写入", r)
		}
		return uint16(r), nil
	case models.Int32:
		r := math.Round(raw)
		if r < math.MinInt32 || r > math.MaxInt32 {
			return nil, fmt.Errorf("反算出的原始值 %.4f 超出 32 位整数范围", r)
		}
		return int32(r), nil
	case models.Float32:
		return float32(raw), nil
	case models.Double64:
		return raw, nil
	default:
		return uint16(math.Round(raw)), nil
	}
}

// dataTypeText 数据类型的中文名，用于报错文案
func dataTypeText(dt models.TagDataType) string {
	switch dt {
	case models.UInt16:
		return "UInt16"
	case models.Int32:
		return "Int32"
	case models.Float32:
		return "Float32"
	case models.Double64:
		return "Double64"
	default:
		return "该类型"
	}
}

// WritePreview 写入预览：把三个口径串起来，让现场在下发前看到
// 原始值、工程值、以及真正要发出去的寄存器序列。
type WritePreview struct {
	OK        bool     `json:"ok"`        // 输入是否可解析
	Error     string   `json:"error"`     // 不可解析时的原因
	Mode      string   `json:"mode"`      // 输入口径
	RawText   string   `json:"rawText"`   // 原始值（寄存器里的内容）
	EngText   string   `json:"engText"`   // 工程值（含单位）
	HexText   string   `json:"hexText"`   // 位模式十六进制
	Registers []string `json:"registers"` // 按本点字节顺序排好的寄存器，如 ["0x0FDB","0x4049"]
	Formula   string   `json:"formula"`   // 反算公式，工程值口径下才有
	RoundNote string   `json:"roundNote"` // 精度损失提示，例如"已就近取整为 1234"
}

// PreviewWrite 解析输入并给出完整预览；解析失败时只填 Error，其余留空。
func PreviewWrite(tag models.TagPoint, input string, mode models.InputMode) WritePreview {
	p := WritePreview{Mode: string(mode)}
	raw, err := ParseWriteInput(tag, input, mode)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	regs := EncodeRegisters(raw, tag)
	p.OK = true
	p.RawText = formatRawText(tag.DataType, raw)
	p.EngText = FormatValue(raw, tag)
	hexText, _ := BaseView(raw, tag.DataType)
	p.HexText = hexText
	p.Registers = make([]string, 0, len(regs))
	for _, r := range regs {
		p.Registers = append(p.Registers, fmt.Sprintf("0x%04X", r))
	}
	if mode == models.InputModeEng {
		if exact, err := engineeringRaw(tag, input); err == nil {
			// 偏移为 0 时括号里只剩"工程值"，拼完要去掉多余空格
			lhs := strings.TrimSpace("工程值 " + formatSignedTerm(tag.Offset))
			p.Formula = fmt.Sprintf("原始值 = (%s) ÷ %s = %s",
				lhs, formatFloat(tag.Scale), formatRawText(tag.DataType, raw))
			if note := roundNote(tag, raw, exact); note != "" {
				p.RoundNote = note
			}
		}
	}
	return p
}

// formatRawText 原始值按类型打印，整数不带小数点
func formatRawText(dt models.TagDataType, raw any) string {
	switch dt {
	case models.UInt16:
		return strconv.FormatUint(uint64(toUint16(raw)), 10)
	case models.Int32:
		return strconv.FormatInt(int64(toInt32(raw)), 10)
	case models.Float32:
		return strconv.FormatFloat(float64(float32(ToFloat(raw))), 'g', -1, 32)
	default:
		return strconv.FormatFloat(ToFloat(raw), 'g', -1, 64)
	}
}

// roundNote 工程值反算后如果有取整或精度收敛，给出一句提示
func roundNote(tag models.TagPoint, raw any, exact float64) string {
	switch tag.DataType {
	case models.UInt16, models.Int32:
		r := ToFloat(raw)
		if math.Abs(exact-r) > 1e-9 {
			return fmt.Sprintf("反算结果 %.6g 不是整数，已就近取整为 %.0f", exact, r)
		}
	case models.Float32:
		f := float32(exact)
		if float64(f) != exact {
			return fmt.Sprintf("单精度存放，%.10g 会被收敛为 %.9g", exact, float64(f))
		}
	}
	return ""
}

// formatFloat 去掉多余的尾随零，公式里看着干净些
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// formatSignedTerm 把偏移项写成 "− 0.5" / "+ 2" 的形式，0 时省略
func formatSignedTerm(v float64) string {
	if v == 0 {
		return ""
	}
	if v > 0 {
		return "− " + formatFloat(v)
	}
	return "+ " + formatFloat(-v)
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
