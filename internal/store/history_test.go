package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendDisabled(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: false})
	if err := h.Append("tag1", time.Now(), 1.5); err != nil {
		t.Fatalf("未启用时 Append 应直接返回: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.Root(), "tag1")); err == nil {
		t.Fatalf("未启用时不应创建目录")
	}
}

func TestAppendAndQuery(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: true, RetentionDays: 30, MaxSizeMB: 10})
	base := time.Date(2026, 10, 9, 10, 0, 0, 0, time.Local)
	for i := 0; i < 100; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		if err := h.Append("tag1", ts, float64(i)); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	pts, err := h.Query("tag1", base.UnixMilli()-1, base.Add(99*time.Second).UnixMilli()+1, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(pts) != 100 {
		t.Fatalf("应查到 100 点，实际 %d", len(pts))
	}
	if pts[0].V != 0 || pts[99].V != 99 {
		t.Fatalf("首尾值不对: %v %v", pts[0], pts[99])
	}
	if pts[0].T != base.UnixMilli() {
		t.Fatalf("时间戳不对: %d", pts[0].T)
	}

	// 时间窗口裁剪
	pts, _ = h.Query("tag1", base.Add(50*time.Second).UnixMilli(), base.Add(59*time.Second).UnixMilli(), 0)
	if len(pts) != 10 {
		t.Fatalf("窗口内应 10 点，实际 %d", len(pts))
	}

	// 降采样：只保留首尾
	pts, _ = h.Query("tag1", 0, 0, 2)
	if len(pts) != 2 {
		t.Fatalf("降采样到 2 点，实际 %d", len(pts))
	}
	if err := h.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
}

func TestQuerySwapsRange(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: true})
	defer h.Close()
	base := time.Now()
	_ = h.Append("t", base, 3.25)
	pts, err := h.Query("t", base.UnixMilli()+5000, base.UnixMilli()-5000, 0)
	if err != nil {
		t.Fatalf("反序区间应自动纠正: %v", err)
	}
	if len(pts) != 1 || pts[0].V != 3.25 {
		t.Fatalf("反序查询结果不对: %+v", pts)
	}
}

func TestNaNIsSkipped(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: true})
	defer h.Close()
	if err := h.Append("t", time.Now(), nan()); err != nil {
		t.Fatalf("NaN 应被忽略而不是报错: %v", err)
	}
	r, _ := h.Range("t")
	if r.Count != 0 {
		t.Fatalf("NaN 不应落盘: %+v", r)
	}
}

func TestRangeAndStats(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: true, RetentionDays: 7, MaxSizeMB: 1})
	defer h.Close()
	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)
	_ = h.Append("t1", yesterday, 1)
	_ = h.Append("t1", today, 2)
	_ = h.Append("t2", today, 3)

	r, err := h.Range("t1")
	if err != nil {
		t.Fatalf("Range 失败: %v", err)
	}
	if r.Count != 2 || r.Bytes != 2*RecordSize {
		t.Fatalf("范围统计不对: %+v", r)
	}
	if r.From != yesterday.UnixMilli() || r.To != today.UnixMilli() {
		t.Fatalf("时间范围不对: %+v", r)
	}

	st := h.Stats()
	if st.TagCount != 2 || st.FileCount != 3 {
		t.Fatalf("库统计不对: %+v", st)
	}
	if st.TotalBytes != 3*RecordSize {
		t.Fatalf("占用字节不对: %d", st.TotalBytes)
	}
	if st.OldestDay == "" || st.NewestDay == "" || st.OldestDay > st.NewestDay {
		t.Fatalf("分片日期范围不对: %s ~ %s", st.OldestDay, st.NewestDay)
	}
}

func TestPruneByRetention(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: true, RetentionDays: 3, MaxSizeMB: 0})
	now := time.Now()
	_ = h.Append("t", now.AddDate(0, 0, -10), 1) // 应被清理
	_ = h.Append("t", now.AddDate(0, 0, -5), 2)  // 应被清理
	_ = h.Append("t", now, 3)                    // 保留
	_ = h.Close()

	removed, freed := h.Prune()
	if removed != 2 {
		t.Fatalf("应清理 2 个过期分片，实际 %d", removed)
	}
	if freed != 2*RecordSize {
		t.Fatalf("释放字节不对: %d", freed)
	}
	r, _ := h.Range("t")
	if r.Count != 1 {
		t.Fatalf("清理后应只剩 1 点: %+v", r)
	}
}

func TestPruneBySize(t *testing.T) {
	// 上限压到最小：每条 16B，MaxSizeMB 最小 1MB 也装得下，这里直接造几千条跨天数据
	root := t.TempDir()
	h := New(root, Settings{Enabled: true, RetentionDays: 0, MaxSizeMB: 1})
	// 预置 3 个旧分片，每个 400KB（25000 条），合计 1.2MB > 1MB
	base := time.Now().AddDate(0, 0, -5)
	for d := 0; d < 3; d++ {
		dir := filepath.Join(root, "t1")
		_ = os.MkdirAll(dir, 0o755)
		path := filepath.Join(dir, base.AddDate(0, 0, d).Format("2006-01-02")+".dat")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 400*1024)
		if _, err := f.Write(buf); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	removed, freed := h.Prune()
	if removed == 0 || freed == 0 {
		t.Fatalf("超容量应触发清理: removed=%d freed=%d", removed, freed)
	}
	st := h.Stats()
	if st.TotalBytes > int64(h.Settings().MaxSizeMB)*1024*1024 {
		t.Fatalf("清理后仍超上限: %d", st.TotalBytes)
	}
}

func TestClearAndClearAll(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: true})
	defer h.Close()
	_ = h.Append("t1", time.Now(), 1)
	_ = h.Append("t2", time.Now(), 2)
	if err := h.Clear("t1"); err != nil {
		t.Fatalf("清除点位失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.Root(), "t1")); err == nil {
		t.Fatalf("t1 目录应被删除")
	}
	if err := h.ClearAll(); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if st := h.Stats(); st.FileCount != 0 || st.TagCount != 0 {
		t.Fatalf("清空后应为空: %+v", st)
	}
}

func TestCrossDayShards(t *testing.T) {
	h := New(t.TempDir(), Settings{Enabled: true})
	// 跨天写入要自动切分片，且查询能跨文件拼起来
	start := time.Date(2026, 10, 8, 23, 59, 50, 0, time.Local)
	for i := 0; i < 20; i++ {
		_ = h.Append("t", start.Add(time.Duration(i)*time.Second), float64(i))
	}
	_ = h.Close()
	pts, err := h.Query("t", start.UnixMilli(), start.Add(20*time.Second).UnixMilli(), 0)
	if err != nil {
		t.Fatalf("跨天查询失败: %v", err)
	}
	if len(pts) != 20 {
		t.Fatalf("跨天应查到 20 点，实际 %d", len(pts))
	}
	st := h.Stats()
	if st.FileCount != 2 {
		t.Fatalf("应生成 2 个分片，实际 %d", st.FileCount)
	}
}

func TestSafeName(t *testing.T) {
	if got := safeName("../../evil"); got == "../../evil" || got == "" {
		t.Fatalf("目录穿越应被过滤: %q", got)
	}
	if got := safeName(""); got == "" {
		t.Fatalf("空 Id 应生成占位名: %q", got)
	}
}

func TestSettingsValidate(t *testing.T) {
	s := Settings{Enabled: true, RetentionDays: -5, MaxSizeMB: -1}.Validate()
	if s.RetentionDays != 0 || s.MaxSizeMB != 0 {
		t.Fatalf("负数应归零: %+v", s)
	}
	s = Settings{RetentionDays: 99999, MaxSizeMB: 999999}.Validate()
	if s.RetentionDays != 3650 || s.MaxSizeMB != 102400 {
		t.Fatalf("超大值应封顶: %+v", s)
	}
}

func nan() float64 {
	z := 0.0
	return z / z
}
