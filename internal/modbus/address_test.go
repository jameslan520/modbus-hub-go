package modbus

import (
	"strconv"
	"testing"
)

func TestParseAddressDec(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"0", 0},
		{"100", 100},
		{"65535", 65535},
		{" 042 ", 42}, // 前后空格与补零都要容忍
	}
	for _, c := range cases {
		r := ParseAddress(FormatDec, c.in)
		if !r.OK || r.Value != c.want {
			t.Fatalf("ParseAddress(dec, %q) = %+v，期望 %d", c.in, r, c.want)
		}
		if r.Dec != strconv.Itoa(c.want) {
			t.Fatalf("(%q) 十进制显示串不对：%s", c.in, r.Dec)
		}
	}

	bad := []string{"", "  ", "65536", "-1", "abc", "12.5", "1e3"}
	for _, s := range bad {
		if r := ParseAddress(FormatDec, s); r.OK {
			t.Fatalf("ParseAddress(dec, %q) 应该失败，实际 %+v", s, r)
		}
	}
}

// 十进制 >= 40001 极可能是把 PLC 图纸地址当偏移填了，给提示但不拦下
func TestParseAddressDecBigNumberNote(t *testing.T) {
	r := ParseAddress(FormatDec, "40001")
	if !r.OK || r.Value != 40001 {
		t.Fatalf("应解析成功且原样保留 40001：%+v", r)
	}
	if r.Note == "" {
		t.Fatalf("应给出「可能来自 PLC 图纸」的提示：%+v", r)
	}
	if r := ParseAddress(FormatDec, "100"); r.Note != "" {
		t.Fatalf("普通地址不该有提示：%+v", r)
	}
}

func TestParseAddressHex(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"64", 100},
		{"0064", 100},
		{"0x64", 100},
		{"0X64", 100},
		{"64h", 100},
		{"64H", 100},
		{"FFFF", 65535},
		{"ffff", 65535},
		{"0", 0},
	}
	for _, c := range cases {
		r := ParseAddress(FormatHex, c.in)
		if !r.OK || r.Value != c.want {
			t.Fatalf("ParseAddress(hex, %q) = %+v，期望 %d", c.in, r, c.want)
		}
	}
	if r := ParseAddress(FormatHex, "10000"); r.OK {
		t.Fatalf("0x10000 超出 uint16 应失败：%+v", r)
	}
	if r := ParseAddress(FormatHex, "xyz"); r.OK {
		t.Fatalf("非十六进制字符应失败：%+v", r)
	}
}

// 进制标记优先：不管下拉选了什么，写 0x / h 都按十六进制解释
func TestParseAddressHexMarkerOverridesFormat(t *testing.T) {
	r := ParseAddress(FormatDec, "0x10")
	if !r.OK || r.Value != 16 {
		t.Fatalf("十进制模式下写 0x10 应识别为十六进制 16：%+v", r)
	}
	if r.Note == "" {
		t.Fatalf("应提示已按十六进制解释：%+v", r)
	}
	if r := ParseAddress(FormatPLC, "10h"); !r.OK || r.Value != 16 {
		t.Fatalf("PLC 模式下写 10h 应识别为十六进制 16：%+v", r)
	}
}

func TestParseAddressPLC(t *testing.T) {
	cases := []struct {
		in   string
		want int
		area RegisterArea
	}{
		{"40001", 0, AreaHolding},
		{"40002", 1, AreaHolding},
		{"40101", 100, AreaHolding},
		{"49999", 9998, AreaHolding},
		{"400001", 0, AreaHolding},
		{"410001", 10000, AreaHolding},
		{"465536", 65535, AreaHolding},
		{"30001", 0, AreaInput},
		{"00001", 0, AreaCoil},
		{"10001", 0, AreaDiscrete},
	}
	for _, c := range cases {
		r := ParseAddress(FormatPLC, c.in)
		if !r.OK || r.Value != c.want || RegisterArea(r.Area) != c.area {
			t.Fatalf("ParseAddress(plc, %q) = %+v，期望值 %d / 区 %s", c.in, r, c.want, c.area)
		}
	}
}

func TestParseAddressPLCErrors(t *testing.T) {
	bad := []string{
		"4001",   // 位数不够，不能猜
		"40000",  // 5 位写法的偏移必须是 1~9999
		"465537", // 6 位写法的偏移上限 65536
		"99999",  // 首位不是 0/1/3/4
		"4x001",  // 夹字母
		"4-001",
		"",
	}
	for _, s := range bad {
		if r := ParseAddress(FormatPLC, s); r.OK {
			t.Fatalf("ParseAddress(plc, %q) 应该失败，实际 %+v", s, r)
		}
	}
	r := ParseAddress(FormatPLC, "99999")
	if r.Error == "" {
		t.Fatalf("失败时要给出可读原因：%+v", r)
	}
}

// 非保持寄存器区目前用不上（功能码只有 03/06/16），解析成功但要提醒
func TestParseAddressPLCNonHoldingNote(t *testing.T) {
	for _, s := range []string{"30001", "00001", "10001"} {
		r := ParseAddress(FormatPLC, s)
		if !r.OK {
			t.Fatalf("%q 应解析成功：%+v", s, r)
		}
		if r.Note == "" {
			t.Fatalf("%q 应给出该区不受支持的提示：%+v", s, r)
		}
	}
	if r := ParseAddress(FormatPLC, "40001"); r.Note != "" {
		t.Fatalf("保持寄存器不该有提示：%+v", r)
	}
}

// 三种显示形式必须互相自洽：任意一个形式写出来再按该形式读回来，都要还原到同一地址
func TestAddressViewRoundTrip(t *testing.T) {
	addrs := []int{0, 1, 42, 100, 9998, 9999, 10000, 40999, 65534, 65535}
	for _, a := range addrs {
		v := viewAddress(a, AreaHolding)
		if got := ParseAddress(FormatDec, v.Dec); !got.OK || got.Value != a {
			t.Fatalf("地址 %d：十进制 %q 读回 %+v", a, v.Dec, got)
		}
		if got := ParseAddress(FormatHex, v.Hex); !got.OK || got.Value != a {
			t.Fatalf("地址 %d：十六进制 %q 读回 %+v", a, v.Hex, got)
		}
		if got := ParseAddress(FormatPLC, v.PLC); !got.OK || got.Value != a {
			t.Fatalf("地址 %d：PLC %q 读回 %+v", a, v.PLC, got)
		}
		// 再看一遍两种形式的等价性
		if v.Dec != strconv.Itoa(a) {
			t.Fatalf("地址 %d 的十进制串不对：%s", a, v.Dec)
		}
		if len(v.Hex) != 6 {
			t.Fatalf("地址 %d 的十六进制串应为 0x+4 位：%s", a, v.Hex)
		}
	}
}

// PLC 写法的位数分界：偏移 1~9999 用常见的 5 位，再大只能用 6 位
func TestPLCTextBoundary(t *testing.T) {
	if got := plcText(0); got != "40001" {
		t.Fatalf("地址 0 的 PLC 写法应为 40001，实际 %s", got)
	}
	if got := plcText(9998); got != "49999" {
		t.Fatalf("地址 9998 的 PLC 写法应为 49999，实际 %s", got)
	}
	if got := plcText(9999); got != "410000" {
		t.Fatalf("地址 9999 超出 5 位写法上限，应回落 6 位 410000，实际 %s", got)
	}
	if got := plcText(65535); got != "465536" {
		t.Fatalf("地址 65535 的 PLC 写法应为 465536，实际 %s", got)
	}
}
