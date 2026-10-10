package modbus

import (
	"fmt"
	"math"
	"testing"

	"modbus-hub-go/internal/models"
)

// 机器位模式：UInt16 16 位、Float32/Int32 32 位、Double64 64 位
func TestBaseView(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		dt   models.TagDataType
		hex  string
		bin  string
	}{
		{
			name: "UInt16", raw: uint16(0x1234), dt: models.UInt16,
			hex: "0x1234", bin: "0001 0010 0011 0100",
		},
		{
			name: "Int32 负数按补码展示", raw: int32(-1), dt: models.Int32,
			hex: "0xFFFFFFFF", bin: "1111 1111 1111 1111 1111 1111 1111 1111",
		},
		{
			name: "Float32 1.0 的 IEEE754 位", raw: float32(1.0), dt: models.Float32,
			hex: "0x3F800000", bin: "0011 1111 1000 0000 0000 0000 0000 0000",
		},
		{
			name: "Float32 -1.0 符号位为 1", raw: float32(-1.0), dt: models.Float32,
			hex: "0xBF800000", bin: "1011 1111 1000 0000 0000 0000 0000 0000",
		},
		{
			name: "Double64 1.0 的 IEEE754 位", raw: float64(1.0), dt: models.Double64,
			hex: "0x3FF0000000000000",
			bin: "0011 1111 1111 0000 0000 0000 0000 0000 0000 0000 0000 0000 0000 0000 0000 0000",
		},
	}
	for _, c := range cases {
		gotHex, gotBin := BaseView(c.raw, c.dt)
		if gotHex != c.hex {
			t.Fatalf("%s 十六进制应为 %s，得到 %s", c.name, c.hex, gotHex)
		}
		if gotBin != c.bin {
			t.Fatalf("%s 二进制应为 %s，得到 %s", c.name, c.bin, gotBin)
		}
	}
}

// 位模式必须能从 CDAB 的寄存器反推回来，否则界面上的十六进制就是错的
func TestBaseViewMatchesRegisters(t *testing.T) {
	regs := FloatToRegistersCdab(25.5)
	raw := RegistersToFloatCdab(regs)
	want := fmt.Sprintf("0x%08X", math.Float32bits(raw))
	gotHex, _ := BaseView(raw, models.Float32)
	if gotHex != want {
		t.Fatalf("位模式与寄存器不一致：寄存器 %#04X %#04X 期望 %s，得到 %s",
			regs[0], regs[1], want, gotHex)
	}
}

func TestRegistersToHex(t *testing.T) {
	if got := RegistersToHex(nil); got != "" {
		t.Fatalf("空寄存器应返回空串，得到 %q", got)
	}
	if got := RegistersToHex([]uint16{0x0000, 0x3F80}); got != "0x0000 0x3F80" {
		t.Fatalf("寄存器格式错误，得到 %q", got)
	}
}

// 手工核对：1.0f 的 IEEE754 位为 0x3F800000，CDAB 排列应为 [低字 CD, 高字 AB]
func TestFloatCDABKnownVector(t *testing.T) {
	regs := FloatToRegistersCdab(1.0)
	if regs[0] != 0x0000 || regs[1] != 0x3F80 {
		t.Fatalf("1.0 编码错误，得到 %#04X %#04X，期望 0x0000 0x3F80", regs[0], regs[1])
	}
	if got := RegistersToFloatCdab(regs); got != 1.0 {
		t.Fatalf("1.0 解码错误，得到 %v", got)
	}
}

func TestFloatCDABRoundTrip(t *testing.T) {
	for _, v := range []float64{0, 1, -1, 25.5, -1234.5678, 3.4028234e38} {
		f := float32(v)
		regs := FloatToRegistersCdab(float64(f))
		if len(regs) != 2 {
			t.Fatalf("编码长度应为 2，得到 %d", len(regs))
		}
		if got := RegistersToFloatCdab(regs); got != f {
			t.Fatalf("往返失败：%v → %#04X %#04X → %v", f, regs[0], regs[1], got)
		}
	}
}

func TestInt32CDABKnownVector(t *testing.T) {
	// -2 的补码为 0xFFFFFFFE：CD=0xFFFE（reg[0]），AB=0xFFFF（reg[1]）
	regs := Int32ToRegistersCdab(-2)
	if regs[0] != 0xFFFE || regs[1] != 0xFFFF {
		t.Fatalf("-2 编码错误，得到 %#04X %#04X", regs[0], regs[1])
	}
	if got := RegistersToInt32Cdab(regs); got != -2 {
		t.Fatalf("-2 解码错误，得到 %d", got)
	}
}

func TestInt32CDABRoundTrip(t *testing.T) {
	for _, v := range []int32{0, 1, -1, 32767, -32768, 2147483647, -2147483648, 1234567} {
		regs := Int32ToRegistersCdab(v)
		if got := RegistersToInt32Cdab(regs); got != v {
			t.Fatalf("往返失败：%d → %#04X %#04X → %d", v, regs[0], regs[1], got)
		}
	}
}

// GHEF CDAB：寄存器依次 GH、EF、CD、AB，即 reg[0] 为最低位字
func TestDoubleGhefCdabKnownVector(t *testing.T) {
	bits := math.Float64bits(1.0) // 0x3FF0000000000000
	want := []uint16{
		uint16(bits),
		uint16(bits >> 16),
		uint16(bits >> 32),
		uint16(bits >> 48),
	}
	regs := DoubleToRegistersGhefCdab(1.0)
	for i := 0; i < 4; i++ {
		if regs[i] != want[i] {
			t.Fatalf("1.0 第 %d 个字错误：%#04X，期望 %#04X", i, regs[i], want[i])
		}
	}
	if got := RegistersToDoubleGhefCdab(regs); got != 1.0 {
		t.Fatalf("1.0 解码错误，得到 %v", got)
	}
}

func TestDoubleGhefCdabRoundTrip(t *testing.T) {
	for _, v := range []float64{0, 1, -1, 123.456, -98765.4321, 1e-300} {
		regs := DoubleToRegistersGhefCdab(v)
		if got := RegistersToDoubleGhefCdab(regs); got != v {
			t.Fatalf("往返失败：%v → %v → %v", v, regs, got)
		}
	}
}

func TestCRC(t *testing.T) {
	frame := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x0A}
	withCRC := appendCRC(append([]byte{}, frame...))
	if len(withCRC) != 8 {
		t.Fatalf("追加 CRC 后长度应为 8，得到 %d", len(withCRC))
	}
	if !checkCRC(withCRC) {
		t.Fatal("正确帧的 CRC 校验应通过")
	}
	broken := append([]byte{}, withCRC...)
	broken[2] ^= 0xFF
	if checkCRC(broken) {
		t.Fatal("篡改后的帧不应通过 CRC 校验")
	}
	// 标准向量：01 03 00 00 00 0A → CRC 低字节在前 C5 CD
	if withCRC[6] != 0xC5 || withCRC[7] != 0xCD {
		t.Fatalf("CRC 与标准向量不符，得到 %02X %02X，期望 C5 CD", withCRC[6], withCRC[7])
	}
}

func TestSplitReadBlocks(t *testing.T) {
	tags := []models.TagPoint{
		{ID: "a", Address: 0, DataType: models.Float32, SlaveID: 1},
		{ID: "b", Address: 2, DataType: models.Float32, SlaveID: 1},
		{ID: "c", Address: 200, DataType: models.UInt16, SlaveID: 1},
		{ID: "d", Address: 10, DataType: models.UInt16, SlaveID: 2},
	}
	blocks := SplitReadBlocks(tags)
	if len(blocks) != 3 {
		t.Fatalf("应拆成 3 个块（从机1 两块、从机2 一块），得到 %d", len(blocks))
	}
	if blocks[0].SlaveID != 1 || blocks[0].Start != 0 || blocks[0].Count != 4 || len(blocks[0].Tags) != 2 {
		t.Fatalf("第一块错误：slave=%d start=%d count=%d tags=%d",
			blocks[0].SlaveID, blocks[0].Start, blocks[0].Count, len(blocks[0].Tags))
	}
	if blocks[1].Start != 200 || blocks[1].Count != 1 {
		t.Fatalf("第二块错误：start=%d count=%d", blocks[1].Start, blocks[1].Count)
	}
	if blocks[2].SlaveID != 2 {
		t.Fatalf("第三块从机号错误：%d", blocks[2].SlaveID)
	}
}

func TestDecodeFromBlock(t *testing.T) {
	tag := models.TagPoint{ID: "t", Address: 2, DataType: models.Float32, Scale: 1, DecimalPlaces: 2}
	regs := []uint16{0x1111, 0x2222, 0x0000, 0x3F80} // 地址 2 起为 Float32 = 1.0
	v := DecodeFromBlock(tag, regs, 0)
	if v.Quality != "Good" {
		t.Fatalf("质量应为 Good，得到 %s（%s）", v.Quality, v.ErrorMessage)
	}
	if v.DisplayValue != "1.00" {
		t.Fatalf("显示值应为 1.00，得到 %s", v.DisplayValue)
	}
	if v.NumericValue == nil || *v.NumericValue != 1.0 {
		t.Fatalf("数值应为 1.0，得到 %v", v.NumericValue)
	}

	bad := DecodeFromBlock(models.TagPoint{ID: "x", Address: 3, DataType: models.Float32}, regs, 0)
	if bad.Quality != "Bad" {
		t.Fatalf("越界读取应判 Bad，得到 %s", bad.Quality)
	}
}

func TestFormatNumber(t *testing.T) {
	// 与 C# 一致：小数按 -log10 补位，保证不显示成 0.00
	if got := FormatNumber(0.0000123, 2); got != "0.000012" {
		t.Fatalf("小数应自动补位避免显示为 0.00，得到 %s", got)
	}
	if got := FormatNumber(0.000000123, 2); got == "0.00" {
		t.Fatalf("极小值不应被抹平，得到 %s", got)
	}
	if got := FormatNumber(12.3456, 2); got != "12.35" {
		t.Fatalf("定点格式错误，得到 %s", got)
	}
	if got := FormatScientific(1234.5, 2); got != "1.23e+03" {
		t.Fatalf("科学计数格式错误，得到 %s", got)
	}
}

func TestParseInputStripUnit(t *testing.T) {
	tag := models.TagPoint{DataType: models.Float32, Unit: "°C"}
	v, err := ParseInput(tag, "12.5 °C")
	if err != nil {
		t.Fatalf("带单位输入解析失败：%v", err)
	}
	if ToFloat(v) != 12.5 {
		t.Fatalf("解析值错误，得到 %v", ToFloat(v))
	}
	if _, err := ParseInput(tag, "abc"); err == nil {
		t.Fatal("非法输入应报错")
	}
}

func TestEnsureWritableConsecutive(t *testing.T) {
	writable := func(id string, addr uint16) models.TagPoint {
		return models.TagPoint{ID: id, Name: id, Address: addr,
			FunctionCode: models.FCWriteMultipleRegisters, DataType: models.Float32}
	}
	ok := []models.TagPoint{writable("a", 0), writable("b", 2)}
	if err := EnsureWritableConsecutive(ok); err != nil {
		t.Fatalf("首尾相接应通过校验，得到 %v", err)
	}
	gap := []models.TagPoint{writable("a", 0), writable("b", 3)}
	if err := EnsureWritableConsecutive(gap); err == nil {
		t.Fatal("地址不连续应报错")
	}
	ro := []models.TagPoint{writable("a", 0), {ID: "r", Name: "r", Address: 2, FunctionCode: models.FCReadHoldingRegisters}}
	if err := EnsureWritableConsecutive(ro); err == nil {
		t.Fatal("含只读点应报错")
	}
}
