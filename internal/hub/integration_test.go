package hub

import (
	"math"
	"testing"
	"time"

	"modbus-hub-go/internal/models"
	"modbus-hub-go/internal/store"
	"modbus-hub-go/internal/tagio"
)

// newTestHub 造一个不依赖磁盘配置的 Hub：直接塞通道，避免测试读到真实 channels.json
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	h := New(Callbacks{})
	h.mu.Lock()
	h.channels = []models.ChannelConfig{}
	h.mu.Unlock()
	return h
}

func testChannel(id string, tags []models.TagPoint) models.ChannelConfig {
	c := models.NewChannelConfig("测试通道")
	c.ID = id
	c.Tags = tags
	c.SlaveID = 1
	return c
}

func goodValue(tagID string, v float64, ts time.Time) models.TagValue {
	return models.TagValue{
		TagID:        tagID,
		Quality:      "Good",
		DisplayValue: "x",
		NumericValue: &v,
		Timestamp:    models.Now(),
		TimestampMs:  ts.UnixMilli(),
	}
}

// deliver 走完整采集链路：记趋势缓冲 → 判报警 → 落历史，
// 与 ChannelService 回调进来时的行为一致（单独调 handleValues 会漏掉趋势缓冲）
func (h *Hub) deliver(channelID string, values []models.TagValue) {
	h.callbacks().OnValues(channelID, values)
}

func TestHandleValuesFillsAlarmAndTrend(t *testing.T) {
	h := newTestHub(t)
	high := 50.0
	tag := models.NewTagPoint("温度")
	tag.ID = "tag1"
	tag.AlarmEnabled = true
	tag.AlarmHigh = &high
	tag.TrendEnabled = true

	cfg := testChannel("ch1", []models.TagPoint{tag})
	h.mu.Lock()
	h.channels = []models.ChannelConfig{cfg}
	h.mu.Unlock()

	now := time.Now()
	values := []models.TagValue{goodValue("tag1", 80, now)}
	h.deliver("ch1", values)

	if values[0].AlarmState != "High" {
		t.Fatalf("超限应回填 High，实际 %q", values[0].AlarmState)
	}
	if len(h.ActiveAlarms()) != 1 {
		t.Fatalf("应产生 1 条活跃报警，实际 %d", len(h.ActiveAlarms()))
	}
	// 趋势缓冲要记到这个点
	pts := h.Trend("tag1", 0, 0)
	if len(pts) != 1 || !almost(pts[0].V, 80) {
		t.Fatalf("趋势缓冲记录不对: %+v", pts)
	}

	// 回落到限值以内 → 恢复
	values = []models.TagValue{goodValue("tag1", 30, now.Add(time.Second))}
	h.deliver("ch1", values)
	if values[0].AlarmState != "" {
		t.Fatalf("恢复后应清空报警方向，实际 %q", values[0].AlarmState)
	}
	if len(h.ActiveAlarms()) != 0 {
		t.Fatalf("恢复后不应有活跃报警")
	}
	if len(h.AlarmHistory(10)) != 2 {
		t.Fatalf("历史应含 触发+恢复 两条，实际 %d", len(h.AlarmHistory(10)))
	}
}

func TestHandleValuesKeepsAlarmOnBadQuality(t *testing.T) {
	h := newTestHub(t)
	high := 50.0
	tag := models.NewTagPoint("温度")
	tag.ID = "tag1"
	tag.AlarmEnabled = true
	tag.AlarmHigh = &high

	h.mu.Lock()
	h.channels = []models.ChannelConfig{testChannel("ch1", []models.TagPoint{tag})}
	h.mu.Unlock()

	h.deliver("ch1", []models.TagValue{goodValue("tag1", 80, time.Now())})
	bad := []models.TagValue{{TagID: "tag1", Quality: "Bad", DisplayValue: "ERR"}}
	h.deliver("ch1", bad)
	if bad[0].AlarmState != "High" {
		t.Fatalf("采集失败应保持报警方向，实际 %q", bad[0].AlarmState)
	}
}

func TestHandleValuesWritesHistory(t *testing.T) {
	h := newTestHub(t)
	tag := models.NewTagPoint("温度")
	tag.ID = "tag1"
	tag.TrendEnabled = true

	h.mu.Lock()
	h.channels = []models.ChannelConfig{testChannel("ch1", []models.TagPoint{tag})}
	h.mu.Unlock()

	// 默认关闭，不应写盘
	base := time.Now().Add(-time.Hour)
	h.deliver("ch1", []models.TagValue{goodValue("tag1", 11, base)})
	if r, _ := h.HistoryRange("tag1"); r.Count != 0 {
		t.Fatalf("未启用落库时不应写盘: %+v", r)
	}

	if err := h.SetHistorySettings(store.Settings{Enabled: true, RetentionDays: 7, MaxSizeMB: 64}); err != nil {
		t.Fatalf("保存落库策略失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		h.deliver("ch1", []models.TagValue{goodValue("tag1", float64(10+i), ts)})
	}
	h.CloseHistory()

	r, err := h.HistoryRange("tag1")
	if err != nil {
		t.Fatalf("查询范围失败: %v", err)
	}
	if r.Count != 5 {
		t.Fatalf("应落库 5 点，实际 %d", r.Count)
	}
	pts, err := h.QueryHistory("tag1", r.From, r.To+1, 0)
	if err != nil {
		t.Fatalf("查询历史失败: %v", err)
	}
	if len(pts) != 5 || !almost(pts[0].V, 10) || !almost(pts[4].V, 14) {
		t.Fatalf("历史数据不对: %+v", pts)
	}
	// 降采样到 2 点要保留首尾
	pts, _ = h.QueryHistory("tag1", r.From, r.To+1, 2)
	if len(pts) != 2 || !almost(pts[0].V, 10) || !almost(pts[1].V, 14) {
		t.Fatalf("降采样结果不对: %+v", pts)
	}
	_ = h.ClearHistory("tag1")
	if r, _ = h.HistoryRange("tag1"); r.Count != 0 {
		t.Fatalf("清理后应为空: %+v", r)
	}
}

func TestTrendsMultipleTags(t *testing.T) {
	h := newTestHub(t)
	t1 := models.NewTagPoint("温度")
	t1.ID = "tag1"
	t1.Unit = "°C"
	t1.TrendEnabled = true
	t2 := models.NewTagPoint("压力")
	t2.ID = "tag2"
	t2.Unit = "MPa"
	t2.TrendEnabled = true
	high := 100.0
	t2.AlarmEnabled = true
	t2.AlarmHigh = &high

	h.mu.Lock()
	h.channels = []models.ChannelConfig{testChannel("ch1", []models.TagPoint{t1, t2})}
	h.mu.Unlock()

	now := time.Now()
	h.deliver("ch1", []models.TagValue{
		goodValue("tag1", 25, now),
		goodValue("tag2", 0.8, now),
	})

	series := h.Trends([]string{"tag1", "tag2", "不存在"}, 0, 1500)
	if len(series) != 2 {
		t.Fatalf("应返回 2 条序列（不存在的跳过），实际 %d", len(series))
	}
	if series[0].Name != "温度" || series[0].Unit != "°C" || len(series[0].Points) != 1 {
		t.Fatalf("第一条序列不对: %+v", series[0])
	}
	if series[1].High == nil || *series[1].High != 100 {
		t.Fatalf("阈值未带回: %+v", series[1].High)
	}
}

func TestImportTagsModes(t *testing.T) {
	h := newTestHub(t)
	tag := models.NewTagPoint("温度")
	tag.ID = "tag1"
	tag.Address = 0
	cfg := testChannel("ch1", []models.TagPoint{tag})
	h.mu.Lock()
	h.channels = []models.ChannelConfig{cfg}
	h.mu.Unlock()

	incoming := []models.TagPoint{
		{Name: "温度", Address: 0, DataType: models.Float32}, // 合并时命中
		{Name: "压力", Address: 2, DataType: models.Float32}, // 新增
	}
	n, err := h.ImportTags("ch1", incoming, tagio.ModeMerge)
	if err != nil {
		t.Fatalf("合并导入失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("合并后应为 2 个点，实际 %d", n)
	}
	got, _ := h.Find("ch1")
	if got.Tags[0].ID != "tag1" {
		t.Fatalf("合并应保留原 Id: %s", got.Tags[0].ID)
	}

	n, err = h.ImportTags("ch1", incoming, tagio.ModeAppend)
	if err != nil {
		t.Fatalf("追加导入失败: %v", err)
	}
	if n != 4 {
		t.Fatalf("追加后应为 4 个点，实际 %d", n)
	}

	n, err = h.ImportTags("ch1", incoming, tagio.ModeReplace)
	if err != nil {
		t.Fatalf("覆盖导入失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("覆盖后应为 2 个点，实际 %d", n)
	}
	if _, err := h.ImportTags("不存在", incoming, tagio.ModeAppend); err == nil {
		t.Fatalf("通道不存在时应报错")
	}
}

func TestStopReleasesAlarms(t *testing.T) {
	h := newTestHub(t)
	high := 50.0
	tag := models.NewTagPoint("温度")
	tag.ID = "tag1"
	tag.AlarmEnabled = true
	tag.AlarmHigh = &high
	h.mu.Lock()
	h.channels = []models.ChannelConfig{testChannel("ch1", []models.TagPoint{tag})}
	h.mu.Unlock()

	h.deliver("ch1", []models.TagValue{goodValue("tag1", 80, time.Now())})
	if len(h.ActiveAlarms()) != 1 {
		t.Fatalf("应有一条报警")
	}
	h.releaseChannelAlarms("ch1")
	if len(h.ActiveAlarms()) != 0 {
		t.Fatalf("通道停止后应释放报警状态")
	}
}

func almost(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
