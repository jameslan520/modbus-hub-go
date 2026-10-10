package calib

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"modbus-hub-go/internal/modbus"
	"modbus-hub-go/internal/models"
)

// RegisterCount 标定值在该数据类型下占用的寄存器数
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

// ToTyped 把标定用的浮点数转成目标数据类型的值。
// 与 C# CalibrationEncoder.ToTyped 保持一致：四舍五入一律 AwayFromZero，再按类型范围截断。
func ToTyped(v float64, dt models.TagDataType) any {
	switch dt {
	case models.UInt16:
		r := roundClamp(v, 0, 65535)
		return uint16(r)
	case models.Int32:
		r := roundClamp(v, math.MinInt32, math.MaxInt32)
		return int32(r)
	case models.Float32:
		return float32(v)
	case models.Double64:
		return v
	default:
		return v
	}
}

// roundClamp 四舍五入（AwayFromZero）后按 [lo, hi] 截断；NaN 归零
func roundClamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	r := math.Round(v)
	if r < lo {
		return lo
	}
	if r > hi {
		return hi
	}
	return r
}

// EncodeTable 编码标定表：每组先写实测值再写标准值
func EncodeTable(points []models.CalibrationPoint, dt models.TagDataType) []uint16 {
	regs := make([]uint16, 0, len(points)*2*RegisterCount(dt))
	for _, p := range points {
		regs = append(regs, modbus.EncodeRegisters(ToTyped(p.Measured, dt), dt)...)
		regs = append(regs, modbus.EncodeRegisters(ToTyped(p.Standard, dt), dt)...)
	}
	return regs
}

// EncodeSlopeIntercept 编码斜率与截距：先 k 后 b（标准 = k × 实测 + b）
func EncodeSlopeIntercept(slope, intercept float64, dt models.TagDataType) []uint16 {
	regs := make([]uint16, 0, 2*RegisterCount(dt))
	regs = append(regs, modbus.EncodeRegisters(ToTyped(slope, dt), dt)...)
	regs = append(regs, modbus.EncodeRegisters(ToTyped(intercept, dt), dt)...)
	return regs
}

// FitLine 最小二乘拟合 y = k·x + b（x=实测，y=标准）
func FitLine(points []models.CalibrationPoint) (slope, intercept float64, ok bool) {
	if len(points) < 2 {
		return 0, 0, false
	}

	var sx, sy, sxx, sxy float64
	n := float64(len(points))
	for _, p := range points {
		sx += p.Measured
		sy += p.Standard
		sxx += p.Measured * p.Measured
		sxy += p.Measured * p.Standard
	}

	den := n*sxx - sx*sx
	if math.Abs(den) < 1e-18 {
		return 0, 0, false
	}

	slope = (n*sxy - sx*sy) / den
	intercept = (sy - slope*sx) / n
	return slope, intercept, true
}

// BuildDownload 按通道的标定参数生成待写入的寄存器与说明文本
func BuildDownload(mode models.CalibrationWriteMode, dataType models.TagDataType,
	start uint16, points []models.CalibrationPoint) ([]uint16, string, error) {

	var regs []uint16
	var summary string

	if mode == models.CalibrationSlopeIntercept {
		slope, intercept, ok := FitLine(points)
		if !ok {
			return nil, "", errors.New("斜率截距至少需要 2 个实测值不同的标定点")
		}
		regs = EncodeSlopeIntercept(slope, intercept, dataType)
		summary = fmt.Sprintf("内容：斜率 k = %s，截距 b = %s（标准 = k × 实测 + b）",
			Format(slope), Format(intercept))
	} else {
		if len(points) == 0 {
			return nil, "", errors.New("请至少添加 1 个标定点")
		}
		regs = EncodeTable(points, dataType)
		summary = fmt.Sprintf("内容：标定表 %d 组，每组先写实测均值再写标准值", len(points))
	}

	if len(regs) == 0 {
		return nil, "", errors.New("没有可写入的数据")
	}
	if int(start)+len(regs) > 65536 {
		return nil, "", fmt.Errorf("起始地址 %d 加上 %d 个寄存器超出保持寄存器上限 65535，"+
			"请减小起始地址或标定点数量", start, len(regs))
	}
	return regs, summary, nil
}

// Format 标定界面统一的数值显示（对应 C# CalibrationViewModel.FormatValue）
func Format(v float64) string {
	abs := math.Abs(v)
	if abs == 0 {
		return "0"
	}
	if abs >= 1000 {
		return trimFormat(v, 2) // C# "0.##"
	}
	if abs >= 1 {
		return trimFormat(v, 4) // C# "0.####"
	}
	return strconv.FormatFloat(v, 'g', 6, 64) // C# "G6"
}

// trimFormat 定点显示后去掉多余的尾随 0 与小数点
func trimFormat(v float64, digits int) string {
	s := strconv.FormatFloat(v, 'f', digits, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-" {
		s = "0"
	}
	return s
}
