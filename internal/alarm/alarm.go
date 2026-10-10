// Package alarm 点位阈值报警：上下限判定、回差与延时去抖、边沿触发的联动输出。
package alarm

import (
	"fmt"
	"sync"
	"time"

	"modbus-hub-go/internal/models"
)

// maxHistory 报警历史保留条数
const maxHistory = 500

// Event 一条报警记录。Active=true 是正在报警，false 是恢复记录。
type Event struct {
	ID          string  `json:"id"`
	ChannelID   string  `json:"channelId"`
	ChannelName string  `json:"channelName"`
	TagID       string  `json:"tagId"`
	TagName     string  `json:"tagName"`
	Kind        string  `json:"kind"`   // High / Low；恢复记录沿用触发时的方向
	Active      bool    `json:"active"` // true 报警发生，false 已恢复
	Value       float64 `json:"value"`
	Limit       float64 `json:"limit"`
	Unit        string  `json:"unit"`
	Timestamp   string  `json:"timestamp"`
	TimestampMs int64   `json:"timestampMs"`
	Message     string  `json:"message"`
	Acked       bool    `json:"acked"`
	Action      string  `json:"action,omitempty"` // 联动写入的结果描述
}

// tagState 单个点位的判定状态
type tagState struct {
	active bool
	kind   models.AlarmKind
	since  time.Time // 持续超限的起始时刻，配合延时判定
	lastID string    // 触发时生成的事件 ID，恢复时要对上
}

// ActionFunc 报警触发（上升沿）时回调：写联动点位。
// 参数是通道 Id、目标点位 Id 与要写的值文本，返回用于写进报警记录的结果描述。
type ActionFunc func(channelID, tagID, value string) string

// Engine 报警判定引擎，挂在 Hub 上，跨通道共享一份状态
type Engine struct {
	mu       sync.Mutex
	states   map[string]*tagState
	active   map[string]Event
	history  []Event
	onEvent  func(Event)
	onAction ActionFunc
	seq      int64
}

func New(onEvent func(Event), onAction ActionFunc) *Engine {
	return &Engine{
		states:   make(map[string]*tagState),
		active:   make(map[string]Event),
		history:  make([]Event, 0, maxHistory),
		onEvent:  onEvent,
		onAction: onAction,
	}
}

// Evaluate 判定一个点位。value 无效（NaN/Inf）或质量不好时保持原状态。
// 返回当前报警方向，Hub 用它回填 TagValue.AlarmState。
func (e *Engine) Evaluate(channelID, channelName string, tag models.TagPoint,
	value float64, ts time.Time) models.AlarmKind {

	if !tag.AlarmEnabled || !tag.AlarmConfigured() || !tag.IsEnabled {
		e.release(tag.ID, tag.Name, channelID, channelName, value, tag.Unit, ts)
		return models.AlarmNone
	}
	if isBad(value) {
		return e.currentKind(tag.ID)
	}

	kind, limit := breach(tag, value)
	st := e.stateOf(tag.ID)

	if kind == models.AlarmNone {
		// 已报警的点要带上回差才算恢复，避免临界抖动来回触发
		if st.active {
			limitValue, _ := limitOf(tag, st.kind)
			if recovered(st.kind, value, limitValue, tag.AlarmDeadband) {
				e.release(tag.ID, tag.Name, channelID, channelName, value, tag.Unit, ts)
				return models.AlarmNone
			}
			return st.kind
		}
		st.since = time.Time{}
		return models.AlarmNone
	}

	if !st.active {
		if st.since.IsZero() {
			st.since = ts
		}
		delay := time.Duration(tag.AlarmDelaySec) * time.Second
		if ts.Sub(st.since) < delay {
			return models.AlarmNone // 还没持续够，暂不报警
		}
		st.active = true
		st.kind = kind
		ev := e.raise(tag, channelID, channelName, kind, limit, value, ts)
		if e.onAction != nil && tag.AlarmActionTag != "" && tag.AlarmActionText != "" {
			res := e.onAction(channelID, tag.AlarmActionTag, tag.AlarmActionText)
			ev.Action = res
			e.replace(ev)
		}
		return kind
	}

	// 已在报警中：方向切换（越过的限值变了）也要更新记录
	if st.kind != kind {
		e.release(tag.ID, tag.Name, channelID, channelName, value, tag.Unit, ts)
		st.active = true
		st.kind = kind
		st.since = ts
		e.raise(tag, channelID, channelName, kind, limit, value, ts)
	}
	return kind
}

func (e *Engine) stateOf(tagID string) *tagState {
	st, ok := e.states[tagID]
	if !ok {
		st = &tagState{}
		e.states[tagID] = st
	}
	return st
}

func (e *Engine) currentKind(tagID string) models.AlarmKind {
	return e.Kind(tagID)
}

// Kind 查某个点当前的报警方向，不改状态（采集失败时用它保留上一次的报警态）
func (e *Engine) Kind(tagID string) models.AlarmKind {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st, ok := e.states[tagID]; ok && st.active {
		return st.kind
	}
	return models.AlarmNone
}

func (e *Engine) raise(tag models.TagPoint, channelID, channelName string,
	kind models.AlarmKind, limit, value float64, ts time.Time) Event {

	e.mu.Lock()
	e.seq++
	ev := Event{
		ID:          fmt.Sprintf("al-%d-%d", ts.UnixMilli(), e.seq),
		ChannelID:   channelID,
		ChannelName: channelName,
		TagID:       tag.ID,
		TagName:     tag.Name,
		Kind:        string(kind),
		Active:      true,
		Value:       value,
		Limit:       limit,
		Unit:        tag.Unit,
		Timestamp:   ts.Format("2006-01-02 15:04:05"),
		TimestampMs: ts.UnixMilli(),
		Message:     message(tag.Name, kind, limit, value, tag.Unit),
	}
	e.active[tag.ID] = ev
	e.pushLocked(ev)
	st := e.stateOf(tag.ID)
	st.lastID = ev.ID
	e.mu.Unlock()

	if e.onEvent != nil {
		e.onEvent(ev)
	}
	return ev
}

// release 结束一个报警；本来就没报警时什么都不做
func (e *Engine) release(tagID, tagName, channelID, channelName string,
	value float64, unit string, ts time.Time) {

	e.mu.Lock()
	st, ok := e.states[tagID]
	if !ok || !st.active {
		e.mu.Unlock()
		return
	}
	st.active = false
	kind := st.kind
	st.kind = models.AlarmNone
	st.since = time.Time{}
	prev := e.active[tagID]
	delete(e.active, tagID)

	limit, _ := prev.Limit, st.lastID
	e.seq++
	ev := Event{
		ID:          fmt.Sprintf("al-%d-%d", ts.UnixMilli(), e.seq),
		ChannelID:   channelID,
		ChannelName: channelName,
		TagID:       tagID,
		TagName:     tagName,
		Kind:        string(kind),
		Active:      false,
		Value:       value,
		Limit:       limit,
		Unit:        unit,
		Timestamp:   ts.Format("2006-01-02 15:04:05"),
		TimestampMs: ts.UnixMilli(),
		Message:     fmt.Sprintf("%s 已恢复正常（当前 %s）", tagName, numberText(value, unit)),
	}
	e.pushLocked(ev)
	e.mu.Unlock()

	if e.onEvent != nil {
		e.onEvent(ev)
	}
}

// replace 联动结果回填到已发出的事件里
func (e *Engine) replace(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.active[ev.TagID] = ev
	for i := range e.history {
		if e.history[i].ID == ev.ID {
			e.history[i] = ev
			return
		}
	}
}

func (e *Engine) pushLocked(ev Event) {
	e.history = append(e.history, ev)
	if len(e.history) > maxHistory {
		e.history = e.history[len(e.history)-maxHistory:]
	}
}

// Active 当前正在报警的记录
func (e *Engine) Active() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Event, 0, len(e.active))
	for _, ev := range e.active {
		out = append(out, ev)
	}
	sortByTimeDesc(out)
	return out
}

// History 最近的报警历史（含恢复记录），按时间倒序
func (e *Engine) History(limit int) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := len(e.history)
	if limit > 0 && limit < n {
		n = limit
	}
	out := make([]Event, n)
	copy(out, e.history[len(e.history)-n:])
	sortByTimeDesc(out)
	return out
}

// Ack 确认一条报警（只做标记，不清除）
func (e *Engine) Ack(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	ok := false
	for i := range e.history {
		if e.history[i].ID == id {
			e.history[i].Acked = true
			ok = true
		}
	}
	if ev, ok2 := e.active[e.activeTagByID(id)]; ok2 {
		ev.Acked = true
		e.active[ev.TagID] = ev
		ok = true
	}
	return ok
}

func (e *Engine) activeTagByID(id string) string {
	for tagID, ev := range e.active {
		if ev.ID == id {
			return tagID
		}
	}
	return ""
}

// AckAll 确认所有未确认的报警
func (e *Engine) AckAll() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for i := range e.history {
		if !e.history[i].Acked {
			e.history[i].Acked = true
			n++
		}
	}
	for tagID, ev := range e.active {
		if !ev.Acked {
			ev.Acked = true
			e.active[tagID] = ev
		}
	}
	return n
}

// ClearHistory 清空历史，正在报警的保留
func (e *Engine) ClearHistory() {
	e.mu.Lock()
	defer e.mu.Unlock()
	keep := make([]Event, 0, len(e.active))
	for _, ev := range e.active {
		keep = append(keep, ev)
	}
	e.history = keep
}

// Reset 删除点位或通道时清掉它的状态
func (e *Engine) Reset(tagIDs ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range tagIDs {
		delete(e.states, id)
		delete(e.active, id)
	}
}

// Keep 只保留 keep 里的点位状态
func (e *Engine) Keep(keep map[string]bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id := range e.states {
		if !keep[id] {
			delete(e.states, id)
			delete(e.active, id)
		}
	}
}

func breach(tag models.TagPoint, v float64) (models.AlarmKind, float64) {
	if hi, ok := tag.LimitHigh(); ok && v > hi {
		return models.AlarmHigh, hi
	}
	if lo, ok := tag.LimitLow(); ok && v < lo {
		return models.AlarmLow, lo
	}
	return models.AlarmNone, 0
}

func limitOf(tag models.TagPoint, kind models.AlarmKind) (float64, bool) {
	if kind == models.AlarmHigh {
		return tag.LimitHigh()
	}
	return tag.LimitLow()
}

// recovered 带回差的恢复判定：超上限的要回落到 (上限-回差) 以内
func recovered(kind models.AlarmKind, v, limit, deadband float64) bool {
	if kind == models.AlarmHigh {
		return v <= limit-deadband
	}
	return v >= limit+deadband
}

func message(name string, kind models.AlarmKind, limit, v float64, unit string) string {
	dir := "超过上限"
	if kind == models.AlarmLow {
		dir = "低于下限"
	}
	return fmt.Sprintf("%s %s %s（当前 %s）", name, dir, numberText(limit, unit), numberText(v, unit))
}

func numberText(v float64, unit string) string {
	s := fmt.Sprintf("%g", v)
	if unit != "" {
		return s + " " + unit
	}
	return s
}

func isBad(v float64) bool {
	return v != v || v > 1e308 || v < -1e308
}

func sortByTimeDesc(list []Event) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].TimestampMs > list[j-1].TimestampMs; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}
