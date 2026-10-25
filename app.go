package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"modbus-hub-go/internal/alarm"
	"modbus-hub-go/internal/calib"
	"modbus-hub-go/internal/config"
	"modbus-hub-go/internal/export"
	"modbus-hub-go/internal/frames"
	"modbus-hub-go/internal/hub"
	"modbus-hub-go/internal/modbus"
	"modbus-hub-go/internal/models"
	"modbus-hub-go/internal/serialx"
	"modbus-hub-go/internal/store"
	"modbus-hub-go/internal/tagio"
	"modbus-hub-go/internal/trend"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxLogs = 500

// CalibrationRequest 标定下载请求
type CalibrationRequest struct {
	ChannelID    string                      `json:"channelId"`
	StartAddress uint16                      `json:"startAddress"`
	DataType     models.TagDataType          `json:"dataType"`
	ByteOrder    models.ByteOrder            `json:"byteOrder"`
	Mode         models.CalibrationWriteMode `json:"mode"`
	Points       []models.CalibrationPoint   `json:"points"`
}

// FitResult 最小二乘拟合结果
type FitResult struct {
	OK            bool    `json:"ok"`
	Slope         float64 `json:"slope"`
	Intercept     float64 `json:"intercept"`
	SlopeText     string  `json:"slopeText"`
	InterceptText string  `json:"interceptText"`
	Hint          string  `json:"hint"`
}

var csvFilters = []runtime.FileFilter{
	{DisplayName: "CSV 文件 (*.csv)", Pattern: "*.csv"},
	{DisplayName: "全部文件 (*.*)", Pattern: "*.*"},
}

// pickSavePath 弹出保存对话框；用户取消时返回空字符串且无错误
func (a *App) pickSavePath(title, filename string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultDirectory:     config.Dir(),
		DefaultFilename:      filename,
		Title:                title,
		Filters:              csvFilters,
		CanCreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(path), nil
}

// safeFileName 去掉文件名里的非法字符
func safeFileName(name string) string {
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	s := replacer.Replace(strings.TrimSpace(name))
	if s == "" {
		return "export"
	}
	return s
}

// App 暴露给前端的绑定对象
type App struct {
	ctx  context.Context
	hub  *hub.Hub
	mu   sync.Mutex
	logs []models.LogEntry
}

func NewApp() *App {
	return &App{logs: make([]models.LogEntry, 0, maxLogs)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.hub = hub.New(hub.Callbacks{
		OnLog: func(e models.LogEntry) {
			a.appendLog(e)
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "log", e)
			}
		},
		OnValues: func(channelID string, values []models.TagValue) {
			if a.ctx == nil {
				return
			}
			runtime.EventsEmit(a.ctx, "tag-values", map[string]any{
				"channelId": channelID,
				"values":    values,
			})
		},
		OnState: func(channelID string, state models.ChannelState) {
			if a.ctx == nil {
				return
			}
			runtime.EventsEmit(a.ctx, "channel-state", map[string]any{
				"id":    channelID,
				"state": string(state),
			})
		},
		OnAlarm: func(ev alarm.Event) {
			level := "Warn"
			if !ev.Active {
				level = "Info"
			}
			a.emitLog(level, ev.ChannelName, ev.Message)
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "alarm", ev)
			}
		},
	})
}

func (a *App) beforeClose(ctx context.Context) bool {
	if a.hub != nil {
		_ = a.hub.StopAll()
		_ = a.hub.CloseHistory()
	}
	return false
}

func (a *App) appendLog(e models.LogEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs = append(a.logs, e)
	if len(a.logs) > maxLogs {
		a.logs = a.logs[len(a.logs)-maxLogs:]
	}
}

// GetChannels 返回全部通道配置
func (a *App) GetChannels() []models.ChannelConfig {
	return a.hub.Channels()
}

// SaveChannels 保存全部通道配置
func (a *App) SaveChannels(list []models.ChannelConfig) error {
	return a.hub.SaveChannels(list)
}

// NewChannel 生成一个带默认值的通道（前端新增通道时用）
func (a *App) NewChannel(name string) models.ChannelConfig {
	cfg := models.NewChannelConfig(name)
	cfg.Tags = []models.TagPoint{}
	return cfg
}

// NewTag 生成一个带默认值的点（前端新增点表时用）
func (a *App) NewTag(name string) models.TagPoint {
	return models.NewTagPoint(name)
}

// ListPorts 枚举本机串口
func (a *App) ListPorts() []serialx.PortInfo {
	return serialx.List()
}

// GetBaudRates 常用波特率
func (a *App) GetBaudRates() []int {
	return serialx.CommonBaudRates
}

// StartChannel 启动通道
func (a *App) StartChannel(id string) error {
	return a.hub.Start(id)
}

// StopChannel 停止通道
func (a *App) StopChannel(id string) error {
	return a.hub.Stop(id)
}

// StartAll 启动全部启用通道
func (a *App) StartAll() error {
	return a.hub.StartAll()
}

// StopAll 停止全部通道
func (a *App) StopAll() error {
	return a.hub.StopAll()
}

// GetRuntime 通道运行态快照
func (a *App) GetRuntime() []hub.ChannelRuntime {
	return a.hub.Runtime()
}

// GetValues 指定通道的点值快照
func (a *App) GetValues(channelID string) []models.TagValue {
	return a.hub.Values(channelID)
}

// WriteTag 单点写入。mode 取 raw（原始值）/ hex（十六进制位模式）/ eng（工程值），
// 空串按 raw 处理。
func (a *App) WriteTag(channelID, tagID, value string, mode string) error {
	return a.hub.WriteTag(channelID, tagID, value, models.ParseInputMode(mode))
}

// PreviewWrite 写入预览：不下发报文，只把输入口径换算成原始值与寄存器序列。
// 界面靠它在按下"写入"之前核对到底会发出什么。
func (a *App) PreviewWrite(channelID, tagID, value string, mode string) modbus.WritePreview {
	tag, ok := a.hub.FindTag(channelID, tagID)
	if !ok {
		return modbus.WritePreview{Error: "未找到该点位"}
	}
	return modbus.PreviewWrite(tag, value, models.ParseInputMode(mode))
}

// WriteTags 批量连续写入
func (a *App) WriteTags(channelID string, items []hub.WriteTagRequest) error {
	return a.hub.WriteTags(channelID, items)
}

// GetLogs 最近的日志（按时间正序）
func (a *App) GetLogs() []models.LogEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]models.LogEntry, len(a.logs))
	copy(out, a.logs)
	return out
}

// ---------- 趋势曲线 ----------

// GetTrend 取点位在窗口内的曲线数据（已降采样）。maxPoints<=0 时取默认上限。
func (a *App) GetTrend(tagID string, windowSec, maxPoints int) []trend.Point {
	if maxPoints <= 0 {
		maxPoints = 1500
	}
	if maxPoints > 5000 {
		maxPoints = 5000
	}
	return a.hub.Trend(tagID, windowSec, maxPoints)
}

// GetTrendStats 取窗口内的最小值 / 最大值 / 平均值 / 末值
func (a *App) GetTrendStats(tagID string, windowSec int) (trend.Stats, error) {
	st, ok := a.hub.TrendStats(tagID, windowSec)
	if !ok {
		return trend.Stats{}, errors.New("该时间窗口内暂无数据")
	}
	return st, nil
}

// GetTrends 一次取多个点位的曲线（多点位对比）
func (a *App) GetTrends(tagIDs []string, windowSec, maxPoints int) []hub.TrendSeries {
	if maxPoints <= 0 {
		maxPoints = 1500
	}
	if maxPoints > 5000 {
		maxPoints = 5000
	}
	return a.hub.Trends(tagIDs, windowSec, maxPoints)
}

// GetRecentSamples 取点位最近的 n 个采样（标定求平均用）
func (a *App) GetRecentSamples(tagID string, n int) []trend.Point {
	if n <= 0 {
		n = 8
	}
	return a.hub.RecentTrend(tagID, n)
}

// ClearTrend 清空某个点位的曲线
func (a *App) ClearTrend(tagID string) {
	a.hub.ClearTrend(tagID)
}

// ---------- 点位批量导入导出 ----------

// PickTagFile 弹出文件选择框，返回选中的 CSV 路径（取消时为空串）
func (a *App) PickTagFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		DefaultDirectory: config.Dir(),
		Title:            "选择点位 CSV 文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV 文件 (*.csv)", Pattern: "*.csv;*.txt"},
			{DisplayName: "全部文件 (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(path), nil
}

// ParseTagFile 解析点位 CSV，返回可导入的点位与失败行。
// addrFormat 指定地址列的书写形式：dec 十进制 / hex 十六进制 / plc PLC 地址。
func (a *App) ParseTagFile(path, addrFormat string) (tagio.Preview, error) {
	if strings.TrimSpace(path) == "" {
		return tagio.Preview{}, errors.New("未选择文件")
	}
	format, err := resolveAddressFormat(addrFormat)
	if err != nil {
		return tagio.Preview{}, err
	}
	p, err := tagio.ReadFileWith(path, format)
	if err != nil {
		return tagio.Preview{}, err
	}
	if len(p.Tags) == 0 && len(p.Errors) > 0 {
		return p, errors.New("没有解析出可用点位，请查看下方错误列表")
	}
	return p, nil
}

// ParseAddressInput 解析用户输入的寄存器地址，供点参数对话框实时校验与预览。
// 返回体里同时带 Dec / Hex / PLC 三种等价写法，界面一次调用就能全部显示。
func (a *App) ParseAddressInput(addrFormat, text string) (modbus.AddressResult, error) {
	format, err := resolveAddressFormat(addrFormat)
	if err != nil {
		return modbus.AddressResult{}, err
	}
	return modbus.ParseAddress(format, text), nil
}

func resolveAddressFormat(s string) (modbus.AddressFormat, error) {
	switch modbus.AddressFormat(strings.ToLower(strings.TrimSpace(s))) {
	case modbus.FormatDec:
		return modbus.FormatDec, nil
	case modbus.FormatHex:
		return modbus.FormatHex, nil
	case modbus.FormatPLC:
		return modbus.FormatPLC, nil
	default:
		return "", fmt.Errorf("不支持的地址形式：%q", s)
	}
}

// ImportTags 把预览里的点位合并进通道点表，返回合并后的总点数
func (a *App) ImportTags(channelID string, tags []models.TagPoint, mode int) (int, error) {
	if len(tags) == 0 {
		return 0, errors.New("没有可导入的点位")
	}
	n, err := a.hub.ImportTags(channelID, tags, mode)
	if err != nil {
		return 0, err
	}
	modeText := map[int]string{tagio.ModeAppend: "追加", tagio.ModeMerge: "合并", tagio.ModeReplace: "覆盖"}[mode]
	if modeText == "" {
		modeText = "导入"
	}
	a.emitLog("Info", a.channelName(channelID),
		fmt.Sprintf("点位%s完成：导入 %d 个，当前共 %d 个点", modeText, len(tags), n))
	return n, nil
}

// ExportTagTemplate 导出空白模板（含两行示例），供现场批量填写
func (a *App) ExportTagTemplate() (string, error) {
	path, err := a.pickSavePath("导出点位模板", "点位导入模板.csv")
	if err != nil || path == "" {
		return path, err
	}
	if err := export.WriteCSV(path, tagio.Header(), tagio.TemplateRows()); err != nil {
		return "", err
	}
	a.emitLog("Info", "", "已导出点位导入模板："+path)
	return path, nil
}

// ExportTagDefinitions 导出点位定义（不含实时值），导出的文件可以直接再导入。
// 传多个通道时合并成一份，并加一列「通道」区分来源。
func (a *App) ExportTagDefinitions(channelIDs []string) (string, error) {
	groups, err := a.collectChannels(channelIDs)
	if err != nil {
		return "", err
	}
	total := 0
	for _, cfg := range groups {
		total += len(cfg.Tags)
	}
	if total == 0 {
		return "", errors.New("没有点位可导出")
	}
	name := exportFileName(groups)
	path, err := a.pickSavePath("导出点位定义", safeFileName(name)+"-点位定义.csv")
	if err != nil || path == "" {
		return path, err
	}

	multi := len(groups) > 1
	header := tagio.Header()
	if multi {
		header = append([]string{"通道"}, header...)
	}
	rows := make([][]string, 0, total)
	for _, cfg := range groups {
		for _, r := range tagio.Rows(cfg.Tags) {
			if multi {
				r = append([]string{cfg.Name}, r...)
			}
			rows = append(rows, r)
		}
	}
	if err := export.WriteCSV(path, header, rows); err != nil {
		return "", err
	}
	a.emitLog("Info", name, fmt.Sprintf("已导出点位定义 %d 行：%s", len(rows), path))
	return path, nil
}

// collectChannels 按顺序取出待导出的通道，ID 重复时只算一次
func (a *App) collectChannels(channelIDs []string) ([]models.ChannelConfig, error) {
	seen := map[string]bool{}
	groups := make([]models.ChannelConfig, 0, len(channelIDs))
	for _, id := range channelIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cfg, ok := a.hub.Find(id)
		if !ok {
			return nil, errors.New("未找到通道：" + id)
		}
		groups = append(groups, cfg)
	}
	if len(groups) == 0 {
		return nil, errors.New("请先选择通道")
	}
	return groups, nil
}

func exportFileName(groups []models.ChannelConfig) string {
	if len(groups) == 1 {
		return groups[0].Name
	}
	return fmt.Sprintf("%s 等%d个通道", groups[0].Name, len(groups))
}

// ---------- 报警 ----------

// GetActiveAlarms 当前正在报警的记录
func (a *App) GetActiveAlarms() []alarm.Event {
	return a.hub.ActiveAlarms()
}

// GetAlarmHistory 最近的报警历史（含恢复记录）
func (a *App) GetAlarmHistory(limit int) []alarm.Event {
	if limit <= 0 {
		limit = 200
	}
	return a.hub.AlarmHistory(limit)
}

// AckAlarm 确认单条报警
func (a *App) AckAlarm(id string) bool {
	return a.hub.AckAlarm(id)
}

// AckAllAlarms 确认全部报警
func (a *App) AckAllAlarms() int {
	return a.hub.AckAllAlarms()
}

// ClearAlarmHistory 清空报警历史
func (a *App) ClearAlarmHistory() {
	a.hub.ClearAlarmHistory()
}

// ExportAlarms 导出报警历史
func (a *App) ExportAlarms() (string, error) {
	list := a.hub.AlarmHistory(0)
	if len(list) == 0 {
		return "", errors.New("暂无报警记录")
	}
	rows := make([][]string, 0, len(list))
	for _, e := range list {
		state := "恢复"
		if e.Active {
			state = "报警"
		}
		kind := "超上限"
		if e.Kind == "Low" {
			kind = "低于下限"
		}
		rows = append(rows, []string{
			e.Timestamp, e.ChannelName, e.TagName, state, kind,
			strconv.FormatFloat(e.Value, 'f', -1, 64),
			strconv.FormatFloat(e.Limit, 'f', -1, 64),
			e.Unit, boolText(e.Acked), e.Message, e.Action,
		})
	}
	path, err := a.pickSavePath("导出报警记录", "报警记录.csv")
	if err != nil || path == "" {
		return path, err
	}
	header := []string{"时间", "通道", "点位", "状态", "方向", "当前值", "限值",
		"单位", "已确认", "消息", "联动结果"}
	if err := export.WriteCSV(path, header, rows); err != nil {
		return "", err
	}
	return path, nil
}

// ---------- 历史落库 ----------

// GetHistorySettings 落库策略
func (a *App) GetHistorySettings() store.Settings {
	return a.hub.HistorySettings()
}

// SaveHistorySettings 保存落库策略
func (a *App) SaveHistorySettings(s store.Settings) error {
	if err := a.hub.SetHistorySettings(s); err != nil {
		return err
	}
	a.emitLog("Info", "", fmt.Sprintf("历史落库：%s，保留 %d 天，上限 %d MB",
		boolText(s.Enabled), s.RetentionDays, s.MaxSizeMB))
	return nil
}

// QueryHistory 查点位历史数据（毫秒区间），maxPoints<=0 表示不降采样
func (a *App) QueryHistory(tagID string, fromMs, toMs int64, maxPoints int) ([]trend.Point, error) {
	pts, err := a.hub.QueryHistory(tagID, fromMs, toMs, maxPoints)
	if err != nil {
		return []trend.Point{}, err
	}
	return pts, nil
}

// GetHistoryRange 点位历史覆盖的时间范围
func (a *App) GetHistoryRange(tagID string) (store.Range, error) {
	return a.hub.HistoryRange(tagID)
}

// GetHistoryStats 落库占用统计
func (a *App) GetHistoryStats() store.Stats {
	return a.hub.HistoryStats()
}

// ClearHistory 删除某点位的历史数据
func (a *App) ClearHistory(tagID string) error {
	return a.hub.ClearHistory(tagID)
}

// ClearAllHistory 清空整个历史库
func (a *App) ClearAllHistory() error {
	if err := a.hub.ClearAllHistory(); err != nil {
		return err
	}
	a.emitLog("Info", "", "已清空全部历史数据")
	return nil
}

// PruneResult 清理结果
type PruneResult struct {
	Files int   `json:"files"` // 删除的分片数
	Freed int64 `json:"freed"` // 释放的字节数
}

// PruneHistory 立即按保留策略清理一次
func (a *App) PruneHistory() PruneResult {
	files, freed := a.hub.PruneHistory()
	if files > 0 {
		a.emitLog("Info", "", fmt.Sprintf("历史清理：删除 %d 个分片，释放 %.1f MB",
			files, float64(freed)/1024/1024))
	}
	return PruneResult{Files: files, Freed: freed}
}

// ExportHistory 导出点位在指定时间范围内的历史数据
func (a *App) ExportHistory(tagID, tagName string, fromMs, toMs int64) (string, error) {
	pts, err := a.hub.QueryHistory(tagID, fromMs, toMs, 0)
	if err != nil {
		return "", err
	}
	if len(pts) == 0 {
		return "", errors.New("该时间范围内没有历史数据")
	}
	rows := make([][]string, 0, len(pts))
	for _, p := range pts {
		rows = append(rows, []string{
			models.FormatMs(p.T),
			strconv.FormatFloat(p.V, 'f', -1, 64),
		})
	}
	path, err := a.pickSavePath("导出历史数据", safeFileName(tagName)+"-历史.csv")
	if err != nil || path == "" {
		return path, err
	}
	if err := export.WriteCSV(path, []string{"时间", "数值"}, rows); err != nil {
		return "", err
	}
	a.emitLog("Info", "", fmt.Sprintf("已导出历史数据 %d 行：%s", len(rows), path))
	return path, nil
}

// ---------- 通信报文 ----------

// GetFrames 查询通信报文。channelID 为空取全部通道，dir 空串取收发双向。
func (a *App) GetFrames(channelID, dir, keyword string) []frames.Record {
	return a.hub.Frames(channelID, dir, keyword)
}

// GetFrameDetail 取单条报文的逐字段明细（列表里为省流量不带 fields）
func (a *App) GetFrameDetail(seq int) (frames.Record, error) {
	rec, ok := a.hub.FrameDetail(int64(seq))
	if !ok {
		return frames.Record{}, errors.New("该报文已被新数据覆盖，请重新查询")
	}
	return rec, nil
}

// GetFrameStats 报文缓冲概况：条数、容量、收发与异常计数
func (a *App) GetFrameStats() frames.Stats {
	return a.hub.FrameStats()
}

// ClearFrames 清空报文缓冲；channelID 为空时清全部
func (a *App) ClearFrames(channelID string) {
	a.hub.ClearFrames(channelID)
}

// ExportFrames 导出当前过滤条件下的报文为 CSV
func (a *App) ExportFrames(channelID, dir, keyword string) (string, error) {
	recs := a.hub.Frames(channelID, dir, keyword)
	if len(recs) == 0 {
		return "", errors.New("当前过滤条件下没有报文")
	}
	rows := make([][]string, 0, len(recs))
	for _, r := range recs {
		rows = append(rows, []string{
			r.Time,
			frameDirText(r.Direction),
			r.ChannelName,
			frameSlaveText(r.Slave),
			frameFuncText(r.FuncCode),
			strconv.Itoa(r.Length),
			r.Hex,
			r.ASCII,
			r.Summary,
			r.Note,
		})
	}
	name := "通信报文"
	if channelID != "" {
		if c, ok := a.hub.Find(channelID); ok {
			name = safeFileName(c.Name) + "-报文"
		}
	}
	path, err := a.pickSavePath("导出通信报文", name+".csv")
	if err != nil || path == "" {
		return path, err
	}
	header := []string{"时间", "方向", "通道", "从机", "功能码",
		"字节数", "报文（十六进制）", "ASCII", "摘要", "说明"}
	if err := export.WriteCSV(path, header, rows); err != nil {
		return "", err
	}
	a.emitLog("Info", "", fmt.Sprintf("已导出通信报文 %d 行：%s", len(rows), path))
	return path, nil
}

func frameDirText(dir string) string {
	if dir == string(modbus.FrameTx) {
		return "发送"
	}
	return "接收"
}

func frameSlaveText(slave int) string {
	if slave < 0 {
		return "--"
	}
	return strconv.Itoa(slave)
}

func frameFuncText(fc int) string {
	if fc < 0 {
		return "--"
	}
	return fmt.Sprintf("0x%02X", fc)
}

// ---------- 标定 ----------

// FitCalibration 最小二乘拟合标定表，返回斜率与截距
func (a *App) FitCalibration(points []models.CalibrationPoint) FitResult {
	slope, intercept, ok := calib.FitLine(points)
	if !ok {
		hint := "至少 2 个标定点后显示拟合"
		if len(points) >= 2 {
			hint = "实测值相同，无法拟合斜率"
		}
		return FitResult{Hint: hint, SlopeText: "--", InterceptText: "--"}
	}
	return FitResult{
		OK:            true,
		Slope:         slope,
		Intercept:     intercept,
		SlopeText:     calib.Format(slope),
		InterceptText: calib.Format(intercept),
		Hint:          "标准 = k × 实测 + b",
	}
}

// DownloadCalibration 把标定表或斜率截距写入从机保持寄存器，返回写入的寄存器数
func (a *App) DownloadCalibration(req CalibrationRequest) (int, error) {
	regs, summary, err := calib.BuildDownload(req.Mode, req.DataType, req.ByteOrder,
		req.StartAddress, req.Points)
	if err != nil {
		return 0, err
	}
	if err := a.hub.WriteRegisters(req.ChannelID, req.StartAddress, regs); err != nil {
		return 0, err
	}
	a.emitLog("Info", a.channelName(req.ChannelID),
		fmt.Sprintf("标定下载完成：起始地址 %d，%d 个寄存器。%s", req.StartAddress, len(regs), summary))
	return len(regs), nil
}

// ---------- 导出 ----------

// ExportTrend 导出曲线数据（窗口内全量，不降采样）
func (a *App) ExportTrend(tagID, tagName string, windowSec int) (string, error) {
	pts := a.hub.Trend(tagID, windowSec, 0)
	if len(pts) == 0 {
		return "", errors.New("该时间窗口内没有历史数据，先启动通道采集一会儿再导出")
	}
	rows := make([][]string, 0, len(pts))
	for _, p := range pts {
		rows = append(rows, []string{
			models.FormatMs(p.T),
			strconv.FormatFloat(p.V, 'f', -1, 64),
		})
	}
	path, err := a.pickSavePath("导出曲线数据", safeFileName(tagName)+"-曲线.csv")
	if err != nil || path == "" {
		return path, err
	}
	if err := export.WriteCSV(path, []string{"时间", "数值"}, rows); err != nil {
		return "", err
	}
	a.emitLog("Info", "", fmt.Sprintf("已导出曲线数据 %d 行：%s", len(rows), path))
	return path, nil
}

// ExportTags 导出点表与当前值。可以一次传多个通道：合并成一份并加一列「通道」，
// 正好对应界面上「同时监视多个通道」看到的内容。
func (a *App) ExportTags(channelIDs []string) (string, error) {
	groups, err := a.collectChannels(channelIDs)
	if err != nil {
		return "", err
	}
	name := exportFileName(groups)
	multi := len(groups) > 1

	path, err := a.pickSavePath("导出点表", safeFileName(name)+"-点表.csv")
	if err != nil || path == "" {
		return path, err
	}

	rows := make([][]string, 0, 64)
	for _, cfg := range groups {
		values := make(map[string]models.TagValue)
		for _, v := range a.hub.Values(cfg.ID) {
			values[v.TagID] = v
		}
		for i, t := range cfg.Tags {
			v := values[t.ID]
			row := []string{
				strconv.Itoa(i + 1),
				t.Name,
				strconv.FormatInt(int64(t.Address), 10),
				t.DataType.String(),
				byteOrderOrDash(t),
				fmt.Sprintf("0x%02X", t.FunctionCode),
				unitOrDash(t.Unit),
				strconv.FormatFloat(t.Scale, 'f', -1, 64),
				strconv.FormatFloat(t.Offset, 'f', -1, 64),
				strconv.Itoa(t.DecimalPlaces),
				boolText(t.IsEnabled),
				boolText(t.TrendEnabled),
				displayOrDash(v),
				dashIfEmpty(v.HexValue),
				dashIfEmpty(modbus.RegistersToHex(v.Registers)),
				v.Quality,
				v.Timestamp,
			}
			if multi {
				row = append([]string{cfg.Name}, row...)
			}
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return "", errors.New("没有点位可导出")
	}

	header := []string{"序号", "点位名称", "地址", "数据类型", "字节顺序", "功能码", "单位",
		"倍率", "偏移", "小数位", "启用", "记录曲线", "实时值", "十六进制", "原始寄存器", "质量", "更新时间"}
	if multi {
		header = append([]string{"通道"}, header...)
	}
	if err := export.WriteCSV(path, header, rows); err != nil {
		return "", err
	}
	a.emitLog("Info", name, fmt.Sprintf("已导出点表 %d 行：%s", len(rows), path))
	return path, nil
}

// ExportLogs 导出运行日志
func (a *App) ExportLogs() (string, error) {
	entries := a.GetLogs()
	if len(entries) == 0 {
		return "", errors.New("暂无日志可导出")
	}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{e.Timestamp, e.Level, e.ChannelName, e.Message})
	}
	path, err := a.pickSavePath("导出运行日志", "运行日志.csv")
	if err != nil || path == "" {
		return path, err
	}
	if err := export.WriteCSV(path, []string{"时间", "级别", "通道", "消息"}, rows); err != nil {
		return "", err
	}
	return path, nil
}

// ---------- 内部辅助 ----------

func (a *App) channelName(channelID string) string {
	if cfg, ok := a.hub.Find(channelID); ok {
		return cfg.Name
	}
	return ""
}

// emitLog 写一条日志并推给前端
func (a *App) emitLog(level, channelName, message string) {
	e := models.LogEntry{
		Timestamp:   models.Now(),
		Level:       level,
		ChannelName: channelName,
		Message:     message,
	}
	a.appendLog(e)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "log", e)
	}
}

func unitOrDash(unit string) string {
	if strings.TrimSpace(unit) == "" {
		return "-"
	}
	return unit
}

// byteOrderOrDash 单寄存器类型没有字序可言，导出时留个 "-" 免得看着像漏配
func byteOrderOrDash(t models.TagPoint) string {
	if !t.ByteOrder.Applies(t.DataType) {
		return "-"
	}
	return t.ByteOrder.String()
}

func displayOrDash(v models.TagValue) string {
	if v.DisplayValue == "" {
		return "--"
	}
	return v.DisplayValue
}

// dashIfEmpty 采集失败的点没有位模式与原始寄存器，导出时用 -- 占位
func dashIfEmpty(s string) string {
	if s == "" {
		return "--"
	}
	return s
}

func boolText(v bool) string {
	if v {
		return "是"
	}
	return "否"
}
