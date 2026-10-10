package hub

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"modbus-hub-go/internal/alarm"
	"modbus-hub-go/internal/config"
	"modbus-hub-go/internal/models"
	"modbus-hub-go/internal/store"
	"modbus-hub-go/internal/tagio"
	"modbus-hub-go/internal/trend"
)

// ChannelRuntime 通道运行态（供界面显示）
type ChannelRuntime struct {
	ID      string              `json:"id"`
	State   models.ChannelState `json:"state"`
	TxCount int64               `json:"txCount"`
	RxCount int64               `json:"rxCount"`
	Running bool                `json:"running"`
}

// WriteTagRequest 界面下发的一次写入
type WriteTagRequest struct {
	TagID string `json:"tagId"`
	Value string `json:"value"`
}

// TrendSeries 一条曲线（多点位对比时每个点位一条）
type TrendSeries struct {
	TagID  string         `json:"tagId"`
	Name   string         `json:"name"`
	Unit   string         `json:"unit"`
	High   *float64       `json:"high,omitempty"`
	Low    *float64       `json:"low,omitempty"`
	Points []trend.Point  `json:"points"`
}

type traffic struct {
	tx int64
	rx int64
}

// Hub 多通道管理器
type Hub struct {
	cb       Callbacks
	mu       sync.Mutex
	channels []models.ChannelConfig
	running  map[string]*ChannelService
	traffic  map[string]traffic
	// lastState 记录已停止通道的最终状态，便于界面区分"主动停止"与"启动失败"
	lastState map[string]models.ChannelState
	// trends 挂在 Hub 上而不是 ChannelService 上：通道重启、点表刷新都不会清空历史
	trends *trend.Store
	// alarms 阈值判定引擎，跨通道共享（报警状态不随通道重启丢失）
	alarms *alarm.Engine
	// history 历史落库；未开启时 Append 会直接返回
	history *store.History
}

func New(cb Callbacks) *Hub {
	h := &Hub{
		cb:        cb,
		channels:  config.Load(),
		running:   make(map[string]*ChannelService),
		traffic:   make(map[string]traffic),
		lastState: make(map[string]models.ChannelState),
		trends:    trend.NewStore(),
		history:   store.New(config.HistoryDir(), config.LoadHistorySettings()),
	}
	h.alarms = alarm.New(func(ev alarm.Event) {
		if h.cb.OnAlarm != nil {
			h.cb.OnAlarm(ev)
		}
	}, h.runAlarmAction)
	return h
}

// callbacks 包装外部回调：采集值先落趋势缓冲、判报警、落历史，再抛给界面
func (h *Hub) callbacks() Callbacks {
	return Callbacks{
		OnLog:   h.cb.OnLog,
		OnState: h.cb.OnState,
		OnValues: func(channelID string, values []models.TagValue) {
			h.recordTrend(channelID, values)
			h.handleValues(channelID, values)
			if h.cb.OnValues != nil {
				h.cb.OnValues(channelID, values)
			}
		},
	}
}

// handleValues 报警判定 + 历史落库，并把报警方向回填进 TagValue
func (h *Hub) handleValues(channelID string, values []models.TagValue) {
	if len(values) == 0 {
		return
	}
	h.mu.Lock()
	cfg, ok := h.findLocked(channelID)
	tags := make(map[string]models.TagPoint, len(cfg.Tags))
	for _, t := range cfg.Tags {
		tags[t.ID] = t
	}
	name := cfg.Name
	h.mu.Unlock()
	if !ok {
		return
	}

	record := h.history.Settings().Enabled
	for i := range values {
		tag, ok := tags[values[i].TagID]
		if !ok {
			continue
		}
		// 采集失败时不改判定状态，沿用上一次的报警方向，避免一会儿报一会儿消
		if values[i].Quality != "Good" || values[i].NumericValue == nil {
			values[i].AlarmState = string(h.alarms.Kind(values[i].TagID))
			continue
		}
		v := *values[i].NumericValue
		ts := time.UnixMilli(values[i].TimestampMs)
		if values[i].TimestampMs <= 0 {
			ts = time.Now()
		}
		values[i].AlarmState = string(h.alarms.Evaluate(channelID, name, tag, v, ts))
		if record && tag.IsEnabled && tag.TrendEnabled {
			if err := h.history.Append(tag.ID, ts, v); err != nil && h.cb.OnLog != nil {
				h.cb.OnLog(models.LogEntry{
					Timestamp:   models.Now(),
					Level:       "Warn",
					ChannelName: name,
					Message:     fmt.Sprintf("历史落库失败 %s：%v", tag.Name, err),
				})
			}
		}
	}
}

// runAlarmAction 报警上升沿的联动输出：把预设值写进另一个可写点位
func (h *Hub) runAlarmAction(channelID, tagID, value string) string {
	if err := h.WriteTag(channelID, tagID, value); err != nil {
		return "联动写入失败：" + err.Error()
	}
	return "联动写入 " + value
}

// recordTrend 只记录启用了曲线的点，且仅当本次采集质量良好
func (h *Hub) recordTrend(channelID string, values []models.TagValue) {
	h.mu.Lock()
	enabled := make(map[string]bool, len(values))
	for _, c := range h.channels {
		if c.ID != channelID {
			continue
		}
		for _, t := range c.Tags {
			if t.TrendEnabled && t.IsEnabled {
				enabled[t.ID] = true
			}
		}
		break
	}
	h.mu.Unlock()

	if len(enabled) == 0 {
		return
	}
	for _, v := range values {
		if !enabled[v.TagID] || v.Quality != "Good" || v.NumericValue == nil {
			continue
		}
		stamp := v.TimestampMs
		if stamp <= 0 {
			stamp = time.Now().UnixMilli()
		}
		h.trends.GetOrCreate(v.TagID).Add(time.UnixMilli(stamp), *v.NumericValue)
	}
}

// Trend 取点位在窗口内的曲线数据（已降采样）
func (h *Hub) Trend(tagID string, windowSec, maxPoints int) []trend.Point {
	return h.trends.Snapshot(tagID, time.Duration(windowSec)*time.Second, maxPoints)
}

// TrendStats 取点位在窗口内的统计
func (h *Hub) TrendStats(tagID string, windowSec int) (trend.Stats, bool) {
	return h.trends.Stats(tagID, time.Duration(windowSec)*time.Second)
}

// Trends 一次取多个点位的曲线（多点位对比用）。
// 点名与单位、阈值来自当前配置，点位已被删除时跳过。
func (h *Hub) Trends(tagIDs []string, windowSec, maxPoints int) []TrendSeries {
	meta := make(map[string]models.TagPoint, len(tagIDs))
	h.mu.Lock()
	for _, c := range h.channels {
		for _, t := range c.Tags {
			meta[t.ID] = t
		}
	}
	h.mu.Unlock()

	out := make([]TrendSeries, 0, len(tagIDs))
	for _, id := range tagIDs {
		tag, ok := meta[id]
		if !ok {
			continue
		}
		s := TrendSeries{
			TagID:  id,
			Name:   tag.Name,
			Unit:   tag.Unit,
			High:   tag.AlarmHigh,
			Low:    tag.AlarmLow,
			Points: h.trends.Snapshot(id, time.Duration(windowSec)*time.Second, maxPoints),
		}
		if s.Points == nil {
			s.Points = []trend.Point{}
		}
		out = append(out, s)
	}
	return out
}

// RecentTrend 取点位最近的 n 个采样（标定求均值用）
func (h *Hub) RecentTrend(tagID string, n int) []trend.Point {
	b, ok := h.trends.Get(tagID)
	if !ok {
		return []trend.Point{}
	}
	return b.Recent(n)
}

// ClearTrend 清空单个点位的曲线
func (h *Hub) ClearTrend(tagID string) {
	if b, ok := h.trends.Get(tagID); ok {
		b.Clear()
	}
}

func (h *Hub) Channels() []models.ChannelConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]models.ChannelConfig, len(h.channels))
	copy(out, h.channels)
	return out
}

// SaveChannels 保存配置；运行中的通道按串口参数变化决定是否自动重连
func (h *Hub) SaveChannels(list []models.ChannelConfig) error {
	for i := range list {
		list[i].Sanitize()
	}
	if err := config.Save(list); err != nil {
		return err
	}

	h.mu.Lock()
	old := make(map[string]models.ChannelConfig, len(h.channels))
	for _, c := range h.channels {
		old[c.ID] = c
	}

	var restart []models.ChannelConfig
	for _, cfg := range list {
		svc, isRunning := h.running[cfg.ID]
		prev, existed := old[cfg.ID]
		if !isRunning || !existed {
			continue
		}
		if prev.SerialSignature() != cfg.SerialSignature() {
			restart = append(restart, cfg)
			continue
		}
		svc.SetConfig(cfg)
	}

	// 被删除的通道直接停掉
	var removed []string
	for id := range h.running {
		keep := false
		for _, cfg := range list {
			if cfg.ID == id {
				keep = true
				break
			}
		}
		if !keep {
			removed = append(removed, id)
		}
	}

	h.channels = list
	h.pruneTrendsLocked()
	services := make(map[string]*ChannelService, len(removed))
	for _, id := range removed {
		if svc, ok := h.running[id]; ok {
			services[id] = svc
			delete(h.running, id)
		}
	}
	h.mu.Unlock()

	for id, svc := range services {
		_ = svc.Stop()
		h.mu.Lock()
		h.traffic[id] = traffic{}
		h.mu.Unlock()
	}

	for _, cfg := range restart {
		if err := h.Stop(cfg.ID); err == nil {
			_ = h.Start(cfg.ID)
		}
	}
	return nil
}

// Start 启动指定通道
func (h *Hub) Start(id string) error {
	h.mu.Lock()
	cfg, ok := h.findLocked(id)
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("未找到通道: %s", id)
	}
	if _, running := h.running[id]; running {
		h.mu.Unlock()
		return nil
	}
	if !cfg.IsEnabled {
		h.mu.Unlock()
		return fmt.Errorf("通道 %s 已禁用", cfg.Name)
	}
	svc := NewChannelService(cfg, h.callbacks())
	h.running[id] = svc
	h.mu.Unlock()

	if err := svc.Start(); err != nil {
		h.mu.Lock()
		delete(h.running, id)
		h.lastState[id] = models.StateError
		h.mu.Unlock()
		return err
	}
	return nil
}

// Stop 停止指定通道
func (h *Hub) Stop(id string) error {
	h.mu.Lock()
	svc, ok := h.running[id]
	if !ok {
		h.mu.Unlock()
		return nil
	}
	delete(h.running, id)
	h.mu.Unlock()

	tx, rx := svc.Traffic()
	if err := svc.Stop(); err != nil {
		return err
	}
	h.mu.Lock()
	h.traffic[id] = traffic{tx: tx, rx: rx}
	h.lastState[id] = models.StateDisconnected
	h.mu.Unlock()
	// 通道停了就不再判定报警，但保留历史记录本身
	h.releaseChannelAlarms(id)
	return nil
}

// releaseChannelAlarms 通道停止或删除时清掉它的报警状态。
// 这里不发"已恢复"事件——通道停了不代表现场真的恢复正常。
func (h *Hub) releaseChannelAlarms(channelID string) {
	h.mu.Lock()
	ids := make([]string, 0, 32)
	for _, c := range h.channels {
		if c.ID != channelID {
			continue
		}
		for _, t := range c.Tags {
			ids = append(ids, t.ID)
		}
	}
	h.mu.Unlock()
	if len(ids) > 0 {
		h.alarms.Reset(ids...)
	}
}

// StartAll 启动所有启用通道，单通道失败不影响其他
func (h *Hub) StartAll() error {
	ids := make([]string, 0, len(h.channels))
	h.mu.Lock()
	for _, c := range h.channels {
		if c.IsEnabled {
			ids = append(ids, c.ID)
		}
	}
	h.mu.Unlock()
	for _, id := range ids {
		_ = h.Start(id)
	}
	return nil
}

// StopAll 停止所有运行通道
func (h *Hub) StopAll() error {
	h.mu.Lock()
	ids := make([]string, 0, len(h.running))
	for id := range h.running {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		_ = h.Stop(id)
	}
	return nil
}

// Runtime 各通道运行态快照
func (h *Hub) Runtime() []ChannelRuntime {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]ChannelRuntime, 0, len(h.channels))
	for _, c := range h.channels {
		rt := ChannelRuntime{ID: c.ID, State: models.StateDisconnected}
		if svc, ok := h.running[c.ID]; ok {
			tx, rx := svc.Traffic()
			rt.State = svc.State()
			rt.TxCount = tx
			rt.RxCount = rx
			rt.Running = true
		} else {
			if t, ok := h.traffic[c.ID]; ok {
				rt.TxCount = t.tx
				rt.RxCount = t.rx
			}
			if st, ok := h.lastState[c.ID]; ok {
				rt.State = st
			}
		}
		out = append(out, rt)
	}
	return out
}

// Values 指定通道的点值快照
func (h *Hub) Values(channelID string) []models.TagValue {
	h.mu.Lock()
	svc, ok := h.running[channelID]
	h.mu.Unlock()
	if ok {
		return svc.Values()
	}
	h.mu.Lock()
	var cfg models.ChannelConfig
	for _, c := range h.channels {
		if c.ID == channelID {
			cfg = c
			break
		}
	}
	h.mu.Unlock()
	out := make([]models.TagValue, 0, len(cfg.Tags))
	for _, t := range cfg.Tags {
		// 通道没跑时没有实时值，但报警状态仍要带出去，
		// 否则界面重新加载后点表的报警列会短暂显示成正常
		out = append(out, models.TagValue{
			TagID:        t.ID,
			Quality:      "Bad",
			DisplayValue: "--",
			AlarmState:   string(h.alarms.Kind(t.ID)),
		})
	}
	return out
}

// WriteTag 单点写入
func (h *Hub) WriteTag(channelID, tagID, value string) error {
	h.mu.Lock()
	svc, ok := h.running[channelID]
	h.mu.Unlock()
	if !ok {
		return errors.New("通道未运行")
	}
	return svc.WriteTag(tagID, value)
}

// WriteTags 批量连续写入
func (h *Hub) WriteTags(channelID string, items []WriteTagRequest) error {
	h.mu.Lock()
	svc, ok := h.running[channelID]
	h.mu.Unlock()
	if !ok {
		return errors.New("通道未运行")
	}
	return svc.WriteTags(items)
}

// WriteRegisters 直接写寄存器（标定下载）
func (h *Hub) WriteRegisters(channelID string, start uint16, regs []uint16) error {
	h.mu.Lock()
	svc, ok := h.running[channelID]
	h.mu.Unlock()
	if !ok {
		return errors.New("通道未运行，请先启动通道再下载标定")
	}
	return svc.WriteRegisters(start, regs)
}

// pruneTrendsLocked 丢弃已删除点位的曲线缓冲，避免长期运行后内存只增不减
func (h *Hub) pruneTrendsLocked() {
	keep := make(map[string]bool, 32)
	for _, c := range h.channels {
		for _, t := range c.Tags {
			keep[t.ID] = true
		}
	}
	h.trends.Keep(keep)
	h.alarms.Keep(keep)
}

// ---------- 报警 ----------

// ActiveAlarms 当前正在报警的记录
func (h *Hub) ActiveAlarms() []alarm.Event {
	return h.alarms.Active()
}

// AlarmHistory 最近的报警历史（含恢复记录）
func (h *Hub) AlarmHistory(limit int) []alarm.Event {
	return h.alarms.History(limit)
}

// AckAlarm 确认一条报警
func (h *Hub) AckAlarm(id string) bool {
	return h.alarms.Ack(id)
}

// AckAllAlarms 确认全部报警
func (h *Hub) AckAllAlarms() int {
	return h.alarms.AckAll()
}

// ClearAlarmHistory 清空报警历史，正在报警的保留
func (h *Hub) ClearAlarmHistory() {
	h.alarms.ClearHistory()
}

// AlarmKind 查点位当前报警方向（界面初次加载时补齐染色）
func (h *Hub) AlarmKind(tagID string) string {
	return string(h.alarms.Kind(tagID))
}

// ---------- 历史落库 ----------

// HistorySettings 当前落库策略
func (h *Hub) HistorySettings() store.Settings {
	return h.history.Settings()
}

// SetHistorySettings 更新落库策略并落盘
func (h *Hub) SetHistorySettings(s store.Settings) error {
	if err := config.SaveHistorySettings(s); err != nil {
		return err
	}
	h.history.SetSettings(s)
	return nil
}

// QueryHistory 查点位在 [fromMs, toMs] 内的历史数据
func (h *Hub) QueryHistory(tagID string, fromMs, toMs int64, maxPoints int) ([]trend.Point, error) {
	return h.history.Query(tagID, fromMs, toMs, maxPoints)
}

// HistoryRange 点位历史覆盖的时间范围
func (h *Hub) HistoryRange(tagID string) (store.Range, error) {
	return h.history.Range(tagID)
}

// HistoryStats 落库占用统计
func (h *Hub) HistoryStats() store.Stats {
	return h.history.Stats()
}

// ClearHistory 删除某个点位的历史
func (h *Hub) ClearHistory(tagID string) error {
	return h.history.Clear(tagID)
}

// ClearAllHistory 清空整个历史库
func (h *Hub) ClearAllHistory() error {
	return h.history.ClearAll()
}

// PruneHistory 立即按保留策略清理一次
func (h *Hub) PruneHistory() (int, int64) {
	return h.history.Prune()
}

// ---------- 点位批量导入 ----------

// ImportTags 把 CSV 解析出来的点位合并进通道点表，返回合并后的点数。
// 点位 Id 由后端分配：合并模式匹配上的沿用原 Id，新增的重新生成。
func (h *Hub) ImportTags(channelID string, incoming []models.TagPoint, mode int) (int, error) {
	h.mu.Lock()
	idx := -1
	for i, c := range h.channels {
		if c.ID == channelID {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return 0, fmt.Errorf("未找到通道: %s", channelID)
	}
	cfg := h.channels[idx]
	merged := tagio.Apply(cfg.Tags, incoming, mode, cfg.SlaveID)
	cfg.Tags = merged
	h.channels[idx] = cfg
	h.pruneTrendsLocked()
	h.mu.Unlock()

	if err := config.Save(h.channels); err != nil {
		return 0, err
	}
	// 运行中的通道要拿到新点表，否则新点不会被采集
	if svc, ok := h.running[channelID]; ok {
		svc.SetConfig(cfg)
	}
	return len(merged), nil
}

// ForgetTags 点位删除后清掉它的历史数据
func (h *Hub) ForgetTags(tagIDs ...string) {
	h.history.Forget(tagIDs...)
	h.alarms.Reset(tagIDs...)
}

// CloseHistory 关闭历史库当前打开的分片（退出前调用）
func (h *Hub) CloseHistory() error {
	return h.history.Close()
}

// Find 按 Id 查找通道配置
func (h *Hub) Find(id string) (models.ChannelConfig, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.findLocked(id)
}

func (h *Hub) findLocked(id string) (models.ChannelConfig, bool) {
	for _, c := range h.channels {
		if c.ID == id {
			return c, true
		}
	}
	return models.ChannelConfig{}, false
}
