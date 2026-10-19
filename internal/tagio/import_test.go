package tagio

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"modbus-hub-go/internal/modbus"
	"modbus-hub-go/internal/models"
)

func TestParseWithHeader(t *testing.T) {
	text := "名称,地址,数据类型,功能码,单位,倍率,偏移,小数位,启用,记录曲线\r\n" +
		"温度,0,Float32,03,°C,1,0,1,是,是\r\n" +
		"压力,2,Double64,16,MPa,0.5,-1,3,否,否\r\n"

	p, err := Parse(text, "x.csv")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(p.Errors) != 0 {
		t.Fatalf("不应有错误行: %+v", p.Errors)
	}
	if len(p.Tags) != 2 {
		t.Fatalf("期望 2 个点，实际 %d", len(p.Tags))
	}
	if p.Tags[0].Name != "温度" || p.Tags[0].Address != 0 ||
		p.Tags[0].DataType != models.Float32 || p.Tags[0].Unit != "°C" {
		t.Fatalf("第一个点解析不对: %+v", p.Tags[0])
	}
	if !p.Tags[0].IsEnabled || !p.Tags[0].TrendEnabled {
		t.Fatalf("启用/曲线应为 true: %+v", p.Tags[0])
	}
	second := p.Tags[1]
	if second.DataType != models.Double64 || second.FunctionCode != models.FCWriteMultipleRegisters ||
		second.Scale != 0.5 || second.Offset != -1 || second.DecimalPlaces != 3 {
		t.Fatalf("第二个点解析不对: %+v", second)
	}
	if second.IsEnabled || second.TrendEnabled {
		t.Fatalf("第二个点应为禁用: %+v", second)
	}
}

func TestParseWithoutHeader(t *testing.T) {
	// 无表头按固定列序：序号,名称,从机号,功能码,地址,数据类型,...
	text := "1,温度,1,03,10,Int32,,1,0,2,是,是\n"
	p, err := Parse(text, "x.csv")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(p.Tags) != 1 {
		t.Fatalf("期望 1 个点，实际 %d", len(p.Tags))
	}
	tag := p.Tags[0]
	if tag.Address != 10 || tag.DataType != models.Int32 || tag.SlaveID != 1 {
		t.Fatalf("列序解析不对: %+v", tag)
	}
}

func TestParseBadRows(t *testing.T) {
	text := "名称,地址,数据类型,功能码\r\n" +
		"空地址,,Float32,03\r\n" +
		"坏地址,99999,Float32,03\r\n" +
		"坏类型,3,Unknown,03\r\n" +
		"好点,7,UInt16,06\r\n"
	p, err := Parse(text, "x.csv")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(p.Tags) != 1 {
		t.Fatalf("应只剩一个好点，实际 %d", len(p.Tags))
	}
	if len(p.Errors) != 3 {
		t.Fatalf("期望 3 条错误，实际 %d: %+v", len(p.Errors), p.Errors)
	}
	for _, e := range p.Errors {
		if e.Line < 2 || e.Line > 4 {
			t.Fatalf("错误行号应在 2~4: %d", e.Line)
		}
	}
	if p.Total != 4 {
		t.Fatalf("Total 应统计全部数据行: %d", p.Total)
	}
}

func TestParseSemicolonAndQuotes(t *testing.T) {
	text := "名称;地址;单位\n\"温度, 一号\";5;°C\n"
	p, err := Parse(text, "x.csv")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "温度, 一号" || p.Tags[0].Address != 5 {
		t.Fatalf("分号分隔或引号解析不对: %+v", p.Tags)
	}
}

func TestParseGBK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gbk.csv")
	utf8 := "名称,地址\n温度,3\n"
	gbk, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(utf8))
	if err != nil {
		t.Fatalf("转 GBK 失败: %v", err)
	}
	if err := os.WriteFile(path, gbk, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := ReadFile(path)
	if err != nil {
		t.Fatalf("读 GBK 文件失败: %v", err)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "温度" {
		t.Fatalf("GBK 解码不对: %+v", p.Tags)
	}
}

func TestParseBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.csv")
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("名称,地址\n温度,4\n")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := ReadFile(path)
	if err != nil {
		t.Fatalf("读 BOM 文件失败: %v", err)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "温度" || p.Tags[0].Address != 4 {
		t.Fatalf("BOM 处理不对: %+v", p.Tags)
	}
}

func TestApplyAppend(t *testing.T) {
	existing := []models.TagPoint{{ID: "a", Name: "温度", Address: 0}}
	incoming := []models.TagPoint{{Name: "压力", Address: 2}}
	out := Apply(existing, incoming, ModeAppend, 1)
	if len(out) != 2 {
		t.Fatalf("追加后应为 2 个点: %d", len(out))
	}
	if out[0].ID != "a" {
		t.Fatalf("原点位 Id 应保留: %s", out[0].ID)
	}
	if out[1].ID == "" || out[1].ID == "a" {
		t.Fatalf("新增点应分配新 Id: %s", out[1].ID)
	}
	if out[1].SlaveID != 1 {
		t.Fatalf("从机号应填通道的: %d", out[1].SlaveID)
	}
}

func TestApplyMerge(t *testing.T) {
	existing := []models.TagPoint{
		{ID: "a", Name: "温度", Address: 0, Unit: "°C", Scale: 1},
		{ID: "b", Name: "压力", Address: 2, Scale: 1},
	}
	incoming := []models.TagPoint{
		{Name: "温度", Address: 0, Unit: "K", Scale: 2},   // 更新
		{Name: "流量", Address: 8},                         // 新增
	}
	out := Apply(existing, incoming, ModeMerge, 5)
	if len(out) != 3 {
		t.Fatalf("合并后应为 3 个点: %d", len(out))
	}
	if out[0].ID != "a" || out[0].Unit != "K" || out[0].Scale != 2 {
		t.Fatalf("同名同址应更新且保留 Id: %+v", out[0])
	}
	if out[2].Name != "流量" || out[2].ID == "" {
		t.Fatalf("未匹配的应新增: %+v", out[2])
	}
	if out[0].SlaveID != 5 || out[1].SlaveID != 5 {
		t.Fatalf("合并时从机号应统一: %+v %+v", out[0], out[1])
	}
}

func TestApplyReplace(t *testing.T) {
	existing := []models.TagPoint{{ID: "a", Name: "温度", Address: 0}}
	incoming := []models.TagPoint{{Name: "新点", Address: 1}}
	out := Apply(existing, incoming, ModeReplace, 2)
	if len(out) != 1 || out[0].Name != "新点" || out[0].ID == "" {
		t.Fatalf("覆盖后应只剩导入的点: %+v", out)
	}
}

func TestRowsRoundTrip(t *testing.T) {
	tags := []models.TagPoint{
		{ID: "x", Name: "温度", SlaveID: 3, FunctionCode: models.FCWriteSingleRegister,
			Address: 12, DataType: models.Double64, Unit: "°C", Scale: 1.5,
			Offset: -2, DecimalPlaces: 3, IsEnabled: true, TrendEnabled: false},
	}
	rows := Rows(tags)
	if len(rows) != 1 || len(rows[0]) != len(Header()) {
		t.Fatalf("导出行列数不对: %+v", rows)
	}
	text := ""
	for _, r := range rows {
		for i, c := range r {
			if i > 0 {
				text += ","
			}
			text += c
		}
		text += "\n"
	}
	text = joinLine(Header()) + "\n" + text
	p, err := Parse(text, "x.csv")
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if len(p.Tags) != 1 {
		t.Fatalf("回读点数不对: %d", len(p.Tags))
	}
	got := p.Tags[0]
	want := tags[0]
	if got.Name != want.Name || got.Address != want.Address || got.DataType != want.DataType ||
		got.Scale != want.Scale || got.Offset != want.Offset || got.DecimalPlaces != want.DecimalPlaces ||
		got.FunctionCode != want.FunctionCode || got.SlaveID != want.SlaveID {
		t.Fatalf("往返不一致:\n got=%+v\nwant=%+v", got, want)
	}
	if got.TrendEnabled {
		t.Fatalf("记录曲线应为 false")
	}
}

func joinLine(fields []string) string {
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += ","
		}
		out += f
	}
	return out
}

// 地址列的三种书写形式：整份文件用同一种，由导入界面选定
func TestParseAddressFormats(t *testing.T) {
	text := "名称,地址\r\n" +
		"温度,100\r\n" +
		"压力,200\r\n"

	dec, err := ParseWith(text, "x.csv", modbus.FormatDec)
	if err != nil {
		t.Fatalf("十进制解析失败: %v", err)
	}
	if len(dec.Tags) != 2 || dec.Tags[0].Address != 100 || dec.Tags[1].Address != 200 {
		t.Fatalf("十进制地址不对: %+v", dec.Tags)
	}

	hexText := "名称,地址\r\n温度,0x64\r\n压力,00C8\r\n"
	hexRows, err := ParseWith(hexText, "x.csv", modbus.FormatHex)
	if err != nil {
		t.Fatalf("十六进制解析失败: %v", err)
	}
	if len(hexRows.Tags) != 2 || hexRows.Tags[0].Address != 100 || hexRows.Tags[1].Address != 200 {
		t.Fatalf("十六进制地址不对: %+v", hexRows.Tags)
	}

	plcText := "名称,地址\r\n温度,40101\r\n压力,400201\r\n"
	plcRows, err := ParseWith(plcText, "x.csv", modbus.FormatPLC)
	if err != nil {
		t.Fatalf("PLC 解析失败: %v", err)
	}
	if len(plcRows.Tags) != 2 || plcRows.Tags[0].Address != 100 || plcRows.Tags[1].Address != 200 {
		t.Fatalf("PLC 地址不对: %+v", plcRows.Tags)
	}
}

// 写错的地址要落到错误行并带可读原因，而不是静默变成别的地址
func TestParseBadAddressReported(t *testing.T) {
	bad := parseCSV(t, modbus.FormatPLC, "名称,地址\r\n温度,4001\r\n")
	if len(bad.Errors) != 1 {
		t.Fatalf("PLC 地址位数不够应报错: %+v", bad.Errors)
	}
	if bad.Errors[0].Line != 2 {
		t.Fatalf("错误行号应为 2，实际 %d", bad.Errors[0].Line)
	}

	// 非保持寄存器区本项目读写不了，导入时就拦下
	area := parseCSV(t, modbus.FormatPLC, "名称,地址\r\n液位,30001\r\n")
	if len(area.Errors) != 1 {
		t.Fatalf("输入寄存器区应报错: %+v", area.Errors)
	}

	over := parseCSV(t, modbus.FormatDec, "名称,地址\r\n液位,70000\r\n")
	if len(over.Errors) != 1 {
		t.Fatalf("超范围地址应报错: %+v", over.Errors)
	}
}

func parseCSV(t *testing.T, format modbus.AddressFormat, text string) Preview {
	t.Helper()
	p, err := ParseWith(text, "x.csv", format)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return p
}

func TestParseBoolAndType(t *testing.T) {
	cases := map[string]bool{"是": true, "Y": true, "1": true, "启用": true,
		"否": false, "0": false, "禁用": false}
	for in, want := range cases {
		got, ok := parseBool(in)
		if !ok || got != want {
			t.Fatalf("parseBool(%q)=%v,%v 期望 %v", in, got, ok, want)
		}
	}
	if _, ok := parseBool("也许"); ok {
		t.Fatalf("无法识别的值应返回 false")
	}
	if dt, ok := parseDataType("U16"); !ok || dt != models.UInt16 {
		t.Fatalf("U16 应解析成 UInt16")
	}
	if fc, ok := parseFunctionCode("16 写多个寄存器"); !ok || fc != models.FCWriteMultipleRegisters {
		t.Fatalf("中文功能码解析失败")
	}
}
