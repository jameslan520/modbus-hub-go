package modbus

import (
	"strconv"
	"strings"
)

// 寄存器地址的三种输入形式。
//
// 现场拿到的地址写法很乱：PLC 图纸给 40001，厂家文档给 0x0000，而 Modbus 报文本身
// 用的是 0 起的十进制。这里把「形式」作为输入约定：界面按所选形式解释文本，
// 落库永远是 0 起的 uint16 协议地址，避免同一份配置被不同人写出不同含义。
type AddressFormat string

const (
	FormatDec AddressFormat = "dec" // 十进制（协议地址，0 起）
	FormatHex AddressFormat = "hex" // 十六进制
	FormatPLC AddressFormat = "plc" // PLC 地址（40001 / 400001 形式）
)

// RegisterArea PLC 地址区，由首位数字区分
type RegisterArea string

const (
	AreaCoil     RegisterArea = "coil"     // 0xxxx 线圈（读写位）
	AreaDiscrete RegisterArea = "discrete" // 1xxxx 离散输入（只读位）
	AreaInput    RegisterArea = "input"    // 3xxxx 输入寄存器（只读字）
	AreaHolding  RegisterArea = "holding"  // 4xxxx 保持寄存器（读写字）
)

// maxAddress 协议地址上限。地址在报文里是 uint16，超过 65535 编不进帧。
const maxAddress = 0xFFFF

// plcMaxOffset 各区偏移的最大值：5 位写法最多到 9999，6 位写法到 65536
const plcMaxOffsetShort = 9999
const plcMaxOffsetLong = 65536

func (a RegisterArea) Label() string {
	switch a {
	case AreaCoil:
		return "线圈"
	case AreaDiscrete:
		return "离散输入"
	case AreaInput:
		return "输入寄存器"
	case AreaHolding:
		return "保持寄存器"
	}
	return ""
}

// AddressResult 地址解析结果。Dec / Hex / PLC 是同一个地址的三种等价写法，
// 界面一次解析就能同时展示，省得自己再算一遍。
type AddressResult struct {
	OK    bool   `json:"ok"`
	Value int    `json:"value"` // 0 起的协议地址，0~65535
	Area  string `json:"area"`  // 地址区，解析成功时有效
	Error string `json:"error"` // 解析失败原因，OK 时为空
	Note  string `json:"note"`  // 需要提醒但不拦下的信息，可为空
	Dec   string `json:"dec"`
	Hex   string `json:"hex"`
	PLC   string `json:"plc"`
}

// ParseAddress 按指定形式解释一段地址文本。
//
// 放宽处理：无论选了哪种形式，只要写了明确的进制标记（0x 前缀、h/H 后缀），
// 都按十六进制解释——现场经常直接粘厂商文档里的 0x 写法。
// PLC 形式要求写全 5 位或 6 位（如 40001、400001），位数不够会报错而不是猜，
// 因为 4001 到底是"寄存器 4001"还是"PLC 4001"没有依据判断。
func ParseAddress(format AddressFormat, text string) AddressResult {
	s := strings.TrimSpace(text)
	if s == "" {
		return failAddress("请填写寄存器地址")
	}
	if marked := hexMarkers(s); marked != "" {
		return parseHex(marked, true)
	}
	switch format {
	case FormatHex:
		return parseHex(s, false)
	case FormatPLC:
		return parsePLC(s)
	default:
		return parseDec(s)
	}
}

// hexMarkers 识别出显式的十六进制写法，返回去掉标记后的正文；不是则返回空串
func hexMarkers(s string) string {
	body := strings.TrimSpace(s)
	lower := strings.ToLower(body)
	switch {
	case strings.HasPrefix(lower, "0x"):
		return strings.TrimSpace(body[2:])
	case strings.HasSuffix(lower, "h"):
		return strings.TrimSpace(body[:len(body)-1])
	}
	return ""
}

func parseDec(s string) AddressResult {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return failAddress("「" + ellipsis(s) + "」不是十进制地址，可填 0~65535")
	}
	if v < 0 || v > maxAddress {
		return failAddress("地址超出范围：" + strconv.FormatInt(v, 10) + " 不在 0~65535 之间")
	}
	r := viewAddress(int(v), AreaHolding)
	// 图纸里第 1 个保持寄存器是 40001，直接当十进制填会偏一个量级，提醒一下
	if v >= 40001 {
		r.Note = "填的是 ≥40001 的十进制值；若它来自 PLC 图纸（如 40001 表示第 1 个保持寄存器），请改用 PLC 形式输入"
	}
	return r
}

func parseHex(s string, marked bool) AddressResult {
	body := strings.TrimSpace(s)
	v, err := strconv.ParseInt(body, 16, 64)
	if err != nil {
		return failAddress("「" + ellipsis(body) + "」不是十六进制地址，可填 0~FFFF")
	}
	if v < 0 || v > maxAddress {
		return failAddress("地址超出范围：0x" + strings.ToUpper(strconv.FormatInt(v, 16)) + " 不在 0~FFFF 之间")
	}
	r := viewAddress(int(v), AreaHolding)
	if marked {
		r.Note = "输入带了 0x/h 标记，已按十六进制解释"
	}
	return r
}

func parsePLC(s string) AddressResult {
	digits := s
	if !allDigits(digits) {
		return failAddress("「" + ellipsis(digits) + "」不是 PLC 地址，应写成 40001 或 400001 这样的数字")
	}
	var base, limit int64
	switch len(digits) {
	case 5:
		base, limit = 10000, plcMaxOffsetShort
	case 6:
		base, limit = 100000, plcMaxOffsetLong
	default:
		return failAddress("PLC 地址要写全 5 位或 6 位（如 40001、400001），当前 " +
			strconv.Itoa(len(digits)) + " 位：" + ellipsis(digits))
	}

	var area RegisterArea
	switch digits[0] {
	case '0':
		area = AreaCoil
	case '1':
		area = AreaDiscrete
	case '3':
		area = AreaInput
	case '4':
		area = AreaHolding
	default:
		return failAddress("PLC 地址首位只能是 0/1/3/4（线圈/离散输入/输入寄存器/保持寄存器）：" + ellipsis(digits))
	}

	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return failAddress("PLC 地址无法识别：" + ellipsis(digits))
	}
	offset := n - base*int64(digits[0]-'0')
	if offset < 1 || offset > limit {
		return failAddress("PLC 地址偏移应在 1~" + strconv.FormatInt(limit, 10) + " 之间：" + ellipsis(digits))
	}

	r := viewAddress(int(offset-1), area)
	if area != AreaHolding {
		r.Note = area.Label() + "区要用别的功能码读写，本项目当前只支持保持寄存器（4xxxx，功能码 03/06/16）"
	}
	return r
}

// viewAddress 拼出同一个地址的三种写法
func viewAddress(addr int, area RegisterArea) AddressResult {
	if addr < 0 {
		addr = 0
	}
	if addr > maxAddress {
		addr = maxAddress
	}
	return AddressResult{
		OK:    true,
		Value: addr,
		Area:  string(area),
		Dec:   strconv.Itoa(addr),
		Hex:   "0x" + strings.ToUpper(fmtHex4(addr)),
		PLC:   plcText(addr),
	}
}

func fmtHex4(v int) string {
	s := strconv.FormatInt(int64(v), 16)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// plcText 保持寄存器的 PLC 写法。偏移 1~9999 用常见的 5 位（40001），
// 更大的只能用 6 位写法（400001），因为 5 位装不下 65535。
func plcText(addr int) string {
	if addr <= plcMaxOffsetShort-1 {
		return strconv.Itoa(40001 + addr)
	}
	return strconv.Itoa(400001 + addr)
}

func failAddress(msg string) AddressResult {
	return AddressResult{OK: false, Error: msg}
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func ellipsis(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 16 {
		return s[:16] + "…"
	}
	return s
}
