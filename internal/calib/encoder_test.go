package calib

import (
	"math"
	"testing"

	"modbus-hub-go/internal/models"
)

func TestRegisterCount(t *testing.T) {
	cases := map[models.TagDataType]int{
		models.UInt16:   1,
		models.Float32:  2,
		models.Int32:    2,
		models.Double64: 4,
	}
	for dt, want := range cases {
		if got := RegisterCount(dt); got != want {
			t.Fatalf("%v 应占 %d 个寄存器，实际 %d", dt, want, got)
		}
	}
}

func TestToTypedUInt16Clamp(t *testing.T) {
	cases := []struct {
		in   float64
		want uint16
	}{
		{-5, 0}, // 负数截断到 0
		{0, 0},
		{1.4, 1},
		{1.5, 2}, // AwayFromZero
		{2.5, 3},
		{-1.5, 0}, // 先舍入到 -2 再截断到 0
		{65535, 65535},
		{70000, 65535}, // 超上限截断
	}
	for _, c := range cases {
		if got := ToTyped(c.in, models.UInt16).(uint16); got != c.want {
			t.Fatalf("UInt16(%v) 期望 %d，实际 %d", c.in, c.want, got)
		}
	}
	if got := ToTyped(math.NaN(), models.UInt16).(uint16); got != 0 {
		t.Fatalf("UInt16(NaN) 应为 0，实际 %d", got)
	}
}

func TestToTypedInt32Clamp(t *testing.T) {
	if got := ToTyped(2.5, models.Int32).(int32); got != 3 {
		t.Fatalf("Int32(2.5) 应为 3（AwayFromZero），实际 %d", got)
	}
	if got := ToTyped(-2.5, models.Int32).(int32); got != -3 {
		t.Fatalf("Int32(-2.5) 应为 -3，实际 %d", got)
	}
	big := ToTyped(5e18, models.Int32).(int32)
	if big != math.MaxInt32 {
		t.Fatalf("超上限应截断到 MaxInt32，实际 %d", big)
	}
	small := ToTyped(-5e18, models.Int32).(int32)
	if small != math.MinInt32 {
		t.Fatalf("超下限应截断到 MinInt32，实际 %d", small)
	}
}

func TestFitLine(t *testing.T) {
	// y = 2x + 1
	pts := []models.CalibrationPoint{
		{Measured: 1, Standard: 3},
		{Measured: 2, Standard: 5},
		{Measured: 3, Standard: 7},
		{Measured: 4, Standard: 9},
	}
	k, b, ok := FitLine(pts)
	if !ok {
		t.Fatal("应拟合成功")
	}
	if math.Abs(k-2) > 1e-9 || math.Abs(b-1) > 1e-9 {
		t.Fatalf("拟合结果错误: k=%v b=%v", k, b)
	}

	if _, _, ok := FitLine([]models.CalibrationPoint{{Measured: 1, Standard: 1}}); ok {
		t.Fatal("少于 2 点不应拟合")
	}
	same := []models.CalibrationPoint{
		{Measured: 5, Standard: 1},
		{Measured: 5, Standard: 2},
	}
	if _, _, ok := FitLine(same); ok {
		t.Fatal("实测值全部相同时不应拟合")
	}
}

func TestEncodeTableLayout(t *testing.T) {
	pts := []models.CalibrationPoint{
		{Measured: 1.5, Standard: 10},
		{Measured: 2.5, Standard: 20},
	}
	regs := EncodeTable(pts, models.Float32)
	if len(regs) != 8 {
		t.Fatalf("2 点 × 2 值 × 2 寄存器 = 8，实际 %d", len(regs))
	}
	// 实测 1.5 -> 0x3FC00000，CDAB 字序下低字在前
	if regs[0] != 0x0000 || regs[1] != 0x3FC0 {
		t.Fatalf("1.5 的 CDAB 编码错误: %04X %04X", regs[0], regs[1])
	}
	// 标准 10 -> 0x41200000
	if regs[2] != 0x0000 || regs[3] != 0x4120 {
		t.Fatalf("10 的 CDAB 编码错误: %04X %04X", regs[2], regs[3])
	}
}

func TestEncodeSlopeInterceptLayout(t *testing.T) {
	regs := EncodeSlopeIntercept(1, 0, models.Float32)
	if len(regs) != 4 {
		t.Fatalf("k、b 各 2 寄存器共 4 个，实际 %d", len(regs))
	}
	// 1.0f -> 0x3F800000，CDAB 低字在前
	if regs[0] != 0x0000 || regs[1] != 0x3F80 {
		t.Fatalf("1.0 的 CDAB 编码错误: %04X %04X", regs[0], regs[1])
	}
	if regs[2] != 0x0000 || regs[3] != 0x0000 {
		t.Fatalf("0.0 的编码错误: %04X %04X", regs[2], regs[3])
	}
}

func TestBuildDownload(t *testing.T) {
	pts := []models.CalibrationPoint{
		{Measured: 1, Standard: 3},
		{Measured: 2, Standard: 5},
	}

	regs, summary, err := BuildDownload(models.CalibrationTable, models.Float32, 100, pts)
	if err != nil {
		t.Fatalf("标定表编码失败: %v", err)
	}
	if len(regs) != 8 {
		t.Fatalf("标定表应 8 个寄存器，实际 %d", len(regs))
	}
	if summary == "" {
		t.Fatal("应返回说明文本")
	}

	regs, _, err = BuildDownload(models.CalibrationSlopeIntercept, models.Float32, 0, pts)
	if err != nil {
		t.Fatalf("斜率截距编码失败: %v", err)
	}
	if len(regs) != 4 {
		t.Fatalf("斜率截距应 4 个寄存器，实际 %d", len(regs))
	}

	// 斜率截距但点数不足
	if _, _, err := BuildDownload(models.CalibrationSlopeIntercept, models.Float32, 0, pts[:1]); err == nil {
		t.Fatal("点数不足应报错")
	}
	// 标定表但无点
	if _, _, err := BuildDownload(models.CalibrationTable, models.Float32, 0, nil); err == nil {
		t.Fatal("无标定点应报错")
	}
	// 超出地址上限
	if _, _, err := BuildDownload(models.CalibrationTable, models.Float32, 65530, pts); err == nil {
		t.Fatal("超出地址上限应报错")
	}
}

func TestFormat(t *testing.T) {
	cases := map[float64]string{
		0:         "0",
		1:         "1",
		1.5:       "1.5",
		0.5:       "0.5",
		1234.5678: "1234.57",
		0.0000123: "1.23e-05",
	}
	for in, want := range cases {
		if got := Format(in); got != want {
			t.Fatalf("Format(%v) 期望 %q，实际 %q", in, want, got)
		}
	}
}
