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

// 位模式必须能从寄存器反推回来，否则界面上的十六进制就是错的
func TestBaseViewMatchesRegisters(t *testing.T) {
	for _, order := range allByteOrders() {
		tag := models.TagPoint{DataType: models.Float32, ByteOrder: order}
		regs := EncodeRegisters(25.5, tag)
		raw := ParseRegisters(regs, tag).(float32)
		want := fmt.Sprintf("0x%08X", math.Float32bits(raw))
		gotHex, _ := BaseView(raw, models.Float32)
		if gotHex != want {
			t.Fatalf("%s 位模式与寄存器不一致：%v 期望 %s，得到 %s", order, regs, want, gotHex)
		}
	}
}

func allByteOrders() []models.ByteOrder {
	return []models.ByteOrder{
		models.ByteOrderCDAB, models.ByteOrderABCD,
		models.ByteOrderBADC, models.ByteOrderDCBA,
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

// 32 位已知向量：把 0x3F800000（1.0f）按四种字序摊成寄存器，逐个手工核对。
// A=0x3F B=0x80 C=0x00 D=0x00
func TestFloat32ByteOrderKnownVectors(t *testing.T) {
	cases := []struct {
		order models.ByteOrder
		want  []uint16
	}{
		{models.ByteOrderCDAB, []uint16{0x0000, 0x3F80}}, // [CD][AB]
		{models.ByteOrderABCD, []uint16{0x3F80, 0x0000}}, // [AB][CD]
		{models.ByteOrderBADC, []uint16{0x803F, 0x0000}}, // [BA][DC]
		{models.ByteOrderDCBA, []uint16{0x0000, 0x803F}}, // [DC][BA]
	}
	for _, c := range cases {
		tag := models.TagPoint{DataType: models.Float32, ByteOrder: c.order}
		regs := EncodeRegisters(1.0, tag)
		if len(regs) != 2 {
			t.Fatalf("%s 编码长度应为 2，得到 %d", c.order, len(regs))
		}
		for i := range c.want {
			if regs[i] != c.want[i] {
				t.Fatalf("%s 第 %d 个字：期望 %#04X，得到 %#04X", c.order, i, c.want[i], regs[i])
			}
		}
		if got := ParseRegisters(regs, tag).(float32); got != 1.0 {
			t.Fatalf("%s 解码错误，得到 %v", c.order, got)
		}
	}
}

// 64 位已知向量：1.0 的位为 3F F0 00 00 00 00 00 00（A..H）
func TestDouble64ByteOrderKnownVectors(t *testing.T) {
	cases := []struct {
		order models.ByteOrder
		want  []uint16
	}{
		{models.ByteOrderCDAB, []uint16{0x0000, 0x0000, 0x0000, 0x3FF0}}, // GH EF CD AB
		{models.ByteOrderABCD, []uint16{0x3FF0, 0x0000, 0x0000, 0x0000}}, // AB CD EF GH
		{models.ByteOrderBADC, []uint16{0xF03F, 0x0000, 0x0000, 0x0000}}, // BA DC FE HG
		{models.ByteOrderDCBA, []uint16{0x0000, 0x0000, 0x0000, 0xF03F}}, // HG FE DC BA
	}
	for _, c := range cases {
		tag := models.TagPoint{DataType: models.Double64, ByteOrder: c.order}
		regs := EncodeRegisters(1.0, tag)
		if len(regs) != 4 {
			t.Fatalf("%s 编码长度应为 4，得到 %d", c.order, len(regs))
		}
		for i := range c.want {
			if regs[i] != c.want[i] {
				t.Fatalf("%s 第 %d 个字：期望 %#04X，得到 %#04X", c.order, i, c.want[i], regs[i])
			}
		}
		if got := ParseRegisters(regs, tag).(float64); got != 1.0 {
			t.Fatalf("%s 解码错误，得到 %v", c.order, got)
		}
	}
}

// Int32 已知向量：-2 的补码 0xFFFFFFFE（A=FF B=FF C=FF D=FE）
func TestInt32ByteOrderKnownVectors(t *testing.T) {
	cases := []struct {
		order models.ByteOrder
		want  []uint16
	}{
		{models.ByteOrderCDAB, []uint16{0xFFFE, 0xFFFF}},
		{models.ByteOrderABCD, []uint16{0xFFFF, 0xFFFE}},
		{models.ByteOrderBADC, []uint16{0xFFFF, 0xFEFF}},
		{models.ByteOrderDCBA, []uint16{0xFEFF, 0xFFFF}},
	}
	for _, c := range cases {
		tag := models.TagPoint{DataType: models.Int32, ByteOrder: c.order}
		regs := EncodeRegisters(int32(-2), tag)
		for i := range c.want {
			if regs[i] != c.want[i] {
				t.Fatalf("%s 第 %d 个字：期望 %#04X，得到 %#04X", c.order, i, c.want[i], regs[i])
			}
		}
		if got := ParseRegisters(regs, tag).(int32); got != -2 {
			t.Fatalf("%s 解码错误，得到 %d", c.order, got)
		}
	}
}

// 四种字序 × 三种多寄存器类型都要能原样往返
func TestByteOrderRoundTrip(t *testing.T) {
	for _, order := range allByteOrders() {
		for _, v := range []float64{0, 1, -1, 25.5, -1234.5678, 3.4028234e38} {
			f := float32(v)
			tag := models.TagPoint{DataType: models.Float32, ByteOrder: order}
			regs := EncodeRegisters(float64(f), tag)
			if got := ParseRegisters(regs, tag).(float32); got != f {
				t.Fatalf("Float32 %s 往返失败：%v → %#04X %#04X → %v", order, f, regs[0], regs[1], got)
			}
		}
		for _, v := range []int32{0, 1, -1, 32767, -32768, 2147483647, -2147483648, 1234567} {
			tag := models.TagPoint{DataType: models.Int32, ByteOrder: order}
			regs := EncodeRegisters(v, tag)
			if got := ParseRegisters(regs, tag).(int32); got != v {
				t.Fatalf("Int32 %s 往返失败：%d → %#04X %#04X → %d", order, v, regs[0], regs[1], got)
			}
		}
		for _, v := range []float64{0, 1, -1, 123.456, -98765.4321, 1e-300} {
			tag := models.TagPoint{DataType: models.Double64, ByteOrder: order}
			regs := EncodeRegisters(v, tag)
			if got := ParseRegisters(regs, tag).(float64); got != v {
				t.Fatalf("Double64 %s 往返失败：%v → %v → %v", order, v, regs, got)
			}
		}
	}
}

// 默认字序必须与旧版逐位一致：旧 channels.json 没有 byteOrder 字段，
// 反序列化后是零值，这里锁住"零值 == CDAB"这个兼容性前提。
func TestZeroByteOrderEqualsCDAB(t *testing.T) {
	if models.ByteOrder(0) != models.ByteOrderCDAB {
		t.Fatal("零值必须是 CDAB，否则旧配置文件会被 reinterpret")
	}
	if got := models.ByteOrder(0).String(); got != "CDAB" {
		t.Fatalf("零值名称应为 CDAB，得到 %s", got)
	}
	// 旧行为：Float32 25.5 → 0x41CC0000 → reg[0]=低字 0x0000，reg[1]=高字 0x41CC
	tag := models.TagPoint{DataType: models.Float32}
	regs := EncodeRegisters(25.5, tag)
	if regs[0] != 0x0000 || regs[1] != 0x41CC {
		t.Fatalf("默认字序应与旧版 CDAB 一致，得到 %#04X %#04X，期望 0x0000 0x41CC",
			regs[0], regs[1])
	}
	// 旧行为：Double64 1.0 → reg[0] 为最低位字
	dtag := models.TagPoint{DataType: models.Double64}
	dregs := EncodeRegisters(1.0, dtag)
	bits := math.Float64bits(1.0)
	wantLow := uint16(bits)
	if dregs[0] != wantLow {
		t.Fatalf("默认字序应与旧版 GHEF CDAB 一致，得到 %#04X，期望 %#04X", dregs[0], wantLow)
	}
}

// UInt16 只有一个寄存器，任何字序都不该改变它
func TestUInt16IgnoresByteOrder(t *testing.T) {
	for _, order := range allByteOrders() {
		tag := models.TagPoint{DataType: models.UInt16, ByteOrder: order}
		regs := EncodeRegisters(uint16(0x1234), tag)
		if len(regs) != 1 || regs[0] != 0x1234 {
			t.Fatalf("%s UInt16 应保持原值，得到 %v", order, regs)
		}
		if got := ParseRegisters([]uint16{0x1234}, tag).(uint16); got != 0x1234 {
			t.Fatalf("%s UInt16 解析错误，得到 %#04X", order, got)
		}
	}
}

// 寄存器不足时不能 panic，返回零值
func TestParseRegistersShortInput(t *testing.T) {
	tag := models.TagPoint{DataType: models.Double64, ByteOrder: models.ByteOrderABCD}
	if got := ParseRegisters([]uint16{1, 2}, tag).(float64); got != 0 {
		t.Fatalf("寄存器不足应返回 0，得到 %v", got)
	}
	if got := ParseRegisters(nil, tag).(float64); got != 0 {
		t.Fatalf("空寄存器应返回 0，得到 %v", got)
	}
}

// 未知字序（老配置里出现越界值）要能安全回落，不能崩
func TestInvalidByteOrderFallsBack(t *testing.T) {
	tag := models.TagPoint{DataType: models.Float32, ByteOrder: models.ByteOrder(99)}
	tag.Sanitize(1)
	if tag.ByteOrder != models.ByteOrderCDAB {
		t.Fatalf("非法字序应被 Sanitize 校正为 CDAB，得到 %v", tag.ByteOrder)
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
