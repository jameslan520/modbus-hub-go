// Package tagio 点位表的 CSV 导入与导出。
//
// 列顺序与 C# 版的点表导出保持一致，导入时按表头名识别列位置，
// 因此手工调整过列顺序的表也能读进来。
package tagio

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"modbus-hub-go/internal/models"
)

// 导入模式
const (
	ModeAppend  int = 0 // 追加到末尾
	ModeMerge   int = 1 // 按 名称+地址 匹配更新，其余追加
	ModeReplace int = 2 // 清空后整体替换
)

// RowError 一行解析失败的原因
type RowError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// Preview 解析结果：成功的点 + 失败的行
type Preview struct {
	Path   string             `json:"path"`
	Header []string           `json:"header"`
	Total  int                `json:"total"`
	Tags   []models.TagPoint  `json:"tags"`
	Errors []RowError         `json:"errors"`
}

// Header 导出的列顺序，同时也是无表头文件的解析顺序
func Header() []string {
	return []string{
		"序号", "名称", "从机号", "功能码", "地址", "数据类型",
		"单位", "倍率", "偏移", "小数位", "启用", "记录曲线",
	}
}

// TemplateRows 模板里的两行示例，导入时会被当成真实数据读进来
func TemplateRows() [][]string {
	return [][]string{
		{"1", "温度", "1", "03", "0", "Float32", "°C", "1", "0", "1", "是", "是"},
		{"2", "设定值", "1", "16", "2", "Float32", "°C", "1", "0", "1", "是", "是"},
	}
}

// Rows 把点位转成 CSV 行（不含 ID，导入后按 名称+地址 匹配）
func Rows(tags []models.TagPoint) [][]string {
	rows := make([][]string, 0, len(tags))
	for i, t := range tags {
		rows = append(rows, []string{
			strconv.Itoa(i + 1),
			t.Name,
			strconv.Itoa(int(t.SlaveID)),
			functionCodeText(t.FunctionCode),
			strconv.Itoa(int(t.Address)),
			t.DataType.String(),
			t.Unit,
			formatFloat(t.Scale),
			formatFloat(t.Offset),
			strconv.Itoa(t.DecimalPlaces),
			boolText(t.IsEnabled),
			boolText(t.TrendEnabled),
		})
	}
	return rows
}

// ReadFile 读一个 CSV 文件并解析成点位。
// 编码上先按 UTF-8（兼容带 BOM），失败再按 GBK——Excel 另存的 CSV 中文是 GBK。
func ReadFile(path string) (Preview, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Preview{}, err
	}
	text, err := decode(data)
	if err != nil {
		return Preview{}, err
	}
	return Parse(text, path)
}

func decode(data []byte) (string, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return string(data[3:]), nil
	}
	s := string(data)
	if utf8.ValidString(s) {
		return s, nil
	}
	// GBK / GB18030 → UTF-8
	out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), data)
	if err != nil {
		// 表里通常只有少量非法字节，能转多少用多少，转不出来再报错
		if len(out) == 0 {
			return "", errors.New("文件编码无法识别，请另存为 UTF-8 或 GBK 编码的 CSV")
		}
	}
	return string(out), nil
}

// Parse 解析 CSV 文本。path 只用于回显给界面。
func Parse(text, path string) (Preview, error) {
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = detectComma(text)
	reader.FieldsPerRecord = -1 // 允许行长度不一致，逐行校验时再报错
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		if err == io.ErrUnexpectedEOF || errors.Is(err, csv.ErrBareQuote) ||
			errors.Is(err, csv.ErrQuote) {
			return Preview{}, errors.New("CSV 格式不正确，请检查引号与逗号是否成对")
		}
		return Preview{}, err
	}

	p := Preview{Path: path, Tags: []models.TagPoint{}, Errors: []RowError{}}
	if len(records) == 0 {
		return p, errors.New("文件是空的")
	}

	// 表头：出现"名称"或 name 就认为第一行是表头
	start := 0
	col := defaultColumns()
	if looksLikeHeader(records[0]) {
		start = 1
		p.Header = records[0]
		col, err = mapColumns(records[0])
		if err != nil {
			return p, err
		}
	}

	for i := start; i < len(records); i++ {
		line := i + 1
		row := records[i]
		if isBlank(row) {
			continue
		}
		p.Total++
		tag, ferr := parseRow(row, col, line)
		if ferr != (RowError{}) {
			p.Errors = append(p.Errors, ferr)
			continue
		}
		p.Tags = append(p.Tags, tag)
	}
	return p, nil
}

// columns 各字段在行里的下标，-1 表示该列不存在
type columns struct {
	name    int
	slave   int
	fc      int
	address int
	dtype   int
	unit    int
	scale   int
	offset  int
	decs    int
	enabled int
	trend   int
}

func defaultColumns() columns {
	return columns{name: 1, slave: 2, fc: 3, address: 4, dtype: 5,
		unit: 6, scale: 7, offset: 8, decs: 9, enabled: 10, trend: 11}
}

var headerAliases = map[string][]string{
	"name":    {"名称", "点位名称", "点名", "name", "tag", "tagname"},
	"slave":   {"从机号", "从站号", "站号", "slave", "slaveid", "device"},
	"fc":      {"功能码", "功能", "functioncode", "fc", "function"},
	"address": {"地址", "寄存器地址", "address", "addr", "register"},
	"dtype":   {"数据类型", "类型", "datatype", "type"},
	"unit":    {"单位", "unit"},
	"scale":   {"倍率", "系数", "scale", "gain", "k"},
	"offset":  {"偏移", "offset", "b"},
	"decs":    {"小数位", "小数", "decimalplaces", "decimals", "precision"},
	"enabled": {"启用", "启用采集", "enabled", "enable", "active"},
	"trend":   {"记录曲线", "曲线", "trend", "trendenabled", "history"},
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s)
}

func looksLikeHeader(row []string) bool {
	for _, cell := range row {
		switch norm(cell) {
		case "名称", "点位名称", "点名", "name", "tag", "tagname":
			return true
		}
	}
	return false
}

func mapColumns(row []string) (columns, error) {
	c := columns{name: -1, slave: -1, fc: -1, address: -1, dtype: -1,
		unit: -1, scale: -1, offset: -1, decs: -1, enabled: -1, trend: -1}
	hit := map[string]int{}
	for idx, cell := range row {
		key := norm(cell)
		if key == "" {
			continue
		}
		for field, aliases := range headerAliases {
			for _, a := range aliases {
				if key == a {
					if _, dup := hit[field]; !dup {
						hit[field] = idx
					}
				}
			}
		}
	}
	assign := func(field string, target *int) {
		if idx, ok := hit[field]; ok {
			*target = idx
		}
	}
	assign("name", &c.name)
	assign("slave", &c.slave)
	assign("fc", &c.fc)
	assign("address", &c.address)
	assign("dtype", &c.dtype)
	assign("unit", &c.unit)
	assign("scale", &c.scale)
	assign("offset", &c.offset)
	assign("decs", &c.decs)
	assign("enabled", &c.enabled)
	assign("trend", &c.trend)

	if c.name < 0 || c.address < 0 {
		return c, errors.New("表头缺少必需列：名称、地址")
	}
	return c, nil
}

func cell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func parseRow(row []string, c columns, line int) (models.TagPoint, RowError) {
	tag := models.NewTagPoint("")
	fail := func(msg string) (models.TagPoint, RowError) {
		return models.TagPoint{}, RowError{Line: line, Message: msg}
	}

	name := cell(row, c.name)
	if name == "" {
		return fail("名称为空")
	}
	tag.Name = name

	addrText := cell(row, c.address)
	addr, err := parseUint(addrText)
	if err != nil || addr > 65535 {
		return fail("地址无效：" + orDash(addrText))
	}
	tag.Address = uint16(addr)

	slave := 0
	if s := cell(row, c.slave); s != "" {
		v, err := parseUint(s)
		if err != nil || v < 1 || v > 247 {
			return fail("从机号无效：" + s)
		}
		slave = v
	}

	fc := models.FCReadHoldingRegisters
	if s := cell(row, c.fc); s != "" {
		v, ok := parseFunctionCode(s)
		if !ok {
			return fail("功能码无效：" + s)
		}
		fc = v
	}
	tag.FunctionCode = fc

	dt := models.Float32
	if s := cell(row, c.dtype); s != "" {
		v, ok := parseDataType(s)
		if !ok {
			return fail("数据类型无效：" + s)
		}
		dt = v
	}
	tag.DataType = dt
	tag.Unit = cell(row, c.unit)

	tag.Scale = 1
	if s := cell(row, c.scale); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fail("倍率无效：" + s)
		}
		tag.Scale = v
	}

	tag.Offset = 0
	if s := cell(row, c.offset); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fail("偏移无效：" + s)
		}
		tag.Offset = v
	}

	tag.DecimalPlaces = 2
	if s := cell(row, c.decs); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 || v > 12 {
			return fail("小数位无效：" + s)
		}
		tag.DecimalPlaces = v
	}

	tag.IsEnabled = true
	if s := cell(row, c.enabled); s != "" {
		v, ok := parseBool(s)
		if !ok {
			return fail("启用列无效：" + s)
		}
		tag.IsEnabled = v
	}

	// 记录曲线列缺失时跟着启用走，配了就按配置来
	tag.TrendEnabled = tag.IsEnabled
	if s := cell(row, c.trend); s != "" {
		v, ok := parseBool(s)
		if !ok {
			return fail("记录曲线列无效：" + s)
		}
		tag.TrendEnabled = v
	}

	if slave > 0 {
		tag.SlaveID = byte(slave)
	}
	return tag, RowError{}
}

// Apply 把导入的点合并进现有点表，返回合并结果。
// incoming 里的点还没有 ID，这里统一分配：匹配上的沿用原 ID，新增的重新生成。
// 返回前全体过一遍 Sanitize，其中会把从机号统一成通道的（与保存配置时的口径一致）。
func Apply(existing, incoming []models.TagPoint, mode int, slaveID byte) []models.TagPoint {
	var out []models.TagPoint

	switch mode {
	case ModeReplace:
		out = make([]models.TagPoint, 0, len(incoming))
		for _, t := range incoming {
			t.ID = models.NewTagPoint("").ID
			out = append(out, t)
		}
	case ModeMerge:
		out = make([]models.TagPoint, len(existing))
		copy(out, existing)
		index := make(map[string]int, len(out))
		for i, t := range out {
			index[key(t)] = i
		}
		for _, t := range incoming {
			if i, ok := index[key(t)]; ok {
				t.ID = out[i].ID // 匹配上的沿用原 Id，曲线与历史才不会断
				out[i] = t
				continue
			}
			t.ID = models.NewTagPoint("").ID
			out = append(out, t)
		}
	default:
		out = make([]models.TagPoint, len(existing))
		copy(out, existing)
		for _, t := range incoming {
			t.ID = models.NewTagPoint("").ID
			out = append(out, t)
		}
	}

	for i := range out {
		out[i].Sanitize(slaveID)
	}
	return out
}

func key(t models.TagPoint) string {
	return norm(t.Name) + "@" + strconv.Itoa(int(t.Address))
}

func parseUint(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		v, err := strconv.ParseInt(s[2:], 16, 64)
		return int(v), err
	}
	// 表格里有写成 "12.0" 的，容忍一下
	if v, err := strconv.ParseFloat(s, 64); err == nil && v == float64(int(v)) {
		return int(v), nil
	}
	return strconv.Atoi(s)
}

func parseFunctionCode(s string) (byte, bool) {
	switch norm(s) {
	case "3", "03", "0x03":
		return models.FCReadHoldingRegisters, true
	case "6", "06", "0x06":
		return models.FCWriteSingleRegister, true
	case "16", "0x10", "10":
		return models.FCWriteMultipleRegisters, true
	}
	low := norm(s)
	if strings.Contains(low, "读保持") || strings.Contains(low, "readholding") {
		return models.FCReadHoldingRegisters, true
	}
	if strings.Contains(low, "写单个") || strings.Contains(low, "writesingle") {
		return models.FCWriteSingleRegister, true
	}
	if strings.Contains(low, "写多个") || strings.Contains(low, "writemultiple") {
		return models.FCWriteMultipleRegisters, true
	}
	return 0, false
}

func parseDataType(s string) (models.TagDataType, bool) {
	switch norm(s) {
	case "uint16", "u16", "word", "0":
		return models.UInt16, true
	case "float32", "float", "f32", "single", "1":
		return models.Float32, true
	case "double64", "double", "d64", "2":
		return models.Double64, true
	case "int32", "i32", "long", "3":
		return models.Int32, true
	}
	return 0, false
}

func parseBool(s string) (bool, bool) {
	switch norm(s) {
	case "是", "y", "yes", "true", "1", "启用", "开", "on", "√", "√启用", "需要":
		return true, true
	case "否", "n", "no", "false", "0", "禁用", "关", "off", "×", "不":
		return false, true
	}
	return false, false
}

func functionCodeText(fc byte) string {
	switch fc {
	case models.FCWriteSingleRegister:
		return "06"
	case models.FCWriteMultipleRegisters:
		return "16"
	default:
		return "03"
	}
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func boolText(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(空)"
	}
	return s
}

func isBlank(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// detectComma 按第一行里出现最多的候选分隔符来定，兼容 Excel 导出成分号/制表符的表
func detectComma(text string) rune {
	line := text
	if i := strings.IndexAny(line, "\r\n"); i > 0 {
		line = line[:i]
	}
	best := ','
	bestCount := 0
	for _, c := range []rune{',', ';', '\t', '|'} {
		n := strings.Count(line, string(c))
		if n > bestCount {
			bestCount = n
			best = c
		}
	}
	if bestCount == 0 {
		return ','
	}
	return best
}
