package trend

import (
	"math"
	"testing"
	"time"
)

func TestBufferAddAndCount(t *testing.T) {
	b := NewBuffer(8)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		b.Add(base.Add(time.Duration(i)*time.Second), float64(i))
	}
	if b.Count() != 5 {
		t.Fatalf("应写入 5 点，实际 %d", b.Count())
	}
}

func TestBufferDropsNonFinite(t *testing.T) {
	b := NewBuffer(8)
	base := time.Now()
	b.Add(base, math.NaN())
	b.Add(base.Add(time.Second), math.Inf(1))
	b.Add(base.Add(2*time.Second), math.Inf(-1))
	if b.Count() != 0 {
		t.Fatalf("NaN/Inf 必须丢弃，实际保留 %d 点", b.Count())
	}
}

func TestBufferDropsDuplicateStamp(t *testing.T) {
	b := NewBuffer(8)
	ts := time.Now()
	b.Add(ts, 12.5)
	b.Add(ts, 12.5) // 同一毫秒同值：冗余
	b.Add(ts, 13.0) // 同毫秒不同值：保留
	if got := b.Count(); got != 2 {
		t.Fatalf("同毫秒同值应合并，期望 2 点，实际 %d", got)
	}
}

func TestBufferRingOverwrite(t *testing.T) {
	// NewBuffer 有 32 的下限，这里按实际容量断言
	const cap = 64
	b := NewBuffer(cap)
	if b.Capacity() != cap {
		t.Fatalf("容量应为 %d，实际 %d", cap, b.Capacity())
	}

	total := 150
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < total; i++ {
		b.Add(base.Add(time.Duration(i)*time.Second), float64(i))
	}
	if b.Count() != cap {
		t.Fatalf("超限写入后应保留 %d 点，实际 %d", cap, b.Count())
	}

	pts := b.Snapshot(0, 0)
	if len(pts) != cap {
		t.Fatalf("快照应 %d 点，实际 %d", cap, len(pts))
	}
	// 应保留最后 cap 个点：total-cap .. total-1
	first := float64(total - cap)
	last := float64(total - 1)
	if pts[0].V != first || pts[len(pts)-1].V != last {
		t.Fatalf("应保留最后 %d 个点（%v..%v），实际 %v..%v", cap, first, last, pts[0].V, pts[len(pts)-1].V)
	}
	// 时间必须严格递增
	for i := 1; i < len(pts); i++ {
		if pts[i].T <= pts[i-1].T {
			t.Fatalf("第 %d 点时间未递增", i)
		}
	}
}

func TestBufferMinimumCapacity(t *testing.T) {
	// 过小的容量会被抬到 32，避免边界处取模出错
	if got := NewBuffer(1).Capacity(); got != 32 {
		t.Fatalf("最小容量应为 32，实际 %d", got)
	}
	if got := NewBuffer(0).Capacity(); got != 32 {
		t.Fatalf("最小容量应为 32，实际 %d", got)
	}
}

func TestBufferWindow(t *testing.T) {
	b := NewBuffer(64)
	now := time.Now()
	// 每 100ms 一个点，共 20 点，跨度 1.9s
	for i := 0; i < 20; i++ {
		b.Add(now.Add(time.Duration(i)*100*time.Millisecond), float64(i))
	}
	pts := b.Snapshot(time.Second, 0)
	if len(pts) != 11 {
		t.Fatalf("1 秒窗口（含端点）应约 11 点，实际 %d", len(pts))
	}
}

func TestDownsample(t *testing.T) {
	pts := make([]Point, 0, 1000)
	for i := 0; i < 1000; i++ {
		pts = append(pts, Point{T: int64(i), V: float64(i)})
	}

	out := Downsample(pts, 100)
	if len(out) != 100 {
		t.Fatalf("降采样后应 100 点，实际 %d", len(out))
	}
	if out[0].V != 0 || out[99].V != 999 {
		t.Fatalf("必须保留首尾点，实际 %v / %v", out[0].V, out[99].V)
	}

	if got := Downsample(pts, 0); len(got) != 1000 {
		t.Fatalf("maxPoints<=0 应不降采样，实际 %d", len(got))
	}
	if got := Downsample(pts, 1); len(got) != 1 || got[0].V != 999 {
		t.Fatalf("maxPoints==1 应只保留末点，实际 %v", got)
	}
	if got := Downsample(pts, 5000); len(got) != 1000 {
		t.Fatalf("点数不足时不应补齐，实际 %d", len(got))
	}
	if got := Downsample(nil, 10); len(got) != 0 {
		t.Fatalf("空输入应返回空，实际 %v", got)
	}
}

func TestBufferStats(t *testing.T) {
	b := NewBuffer(32)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	values := []float64{1, 2, 3, 4, 5}
	for i, v := range values {
		b.Add(base.Add(time.Duration(i)*time.Second), v)
	}

	st, ok := b.Stats(0)
	if !ok {
		t.Fatal("应有统计结果")
	}
	if st.Min != 1 || st.Max != 5 || st.Last != 5 || st.Count != 5 {
		t.Fatalf("统计错误: %+v", st)
	}
	if math.Abs(st.Avg-3) > 1e-9 {
		t.Fatalf("平均值应为 3，实际 %v", st.Avg)
	}

	// 只取最后 2 秒：剩 4、5
	st, ok = b.Stats(2 * time.Second)
	if !ok {
		t.Fatal("应有统计结果")
	}
	if st.Count != 3 || st.Min != 3 || st.Max != 5 {
		t.Fatalf("窗口统计错误: %+v", st)
	}

	if _, ok := b.Stats(0); !ok {
		t.Fatal("应有统计结果")
	}
	empty := NewBuffer(8)
	if _, ok := empty.Stats(0); ok {
		t.Fatal("空缓冲不应返回统计")
	}
}

func TestStoreReuseAndPrune(t *testing.T) {
	s := NewStore()
	b1 := s.GetOrCreate("tag-1")
	b2 := s.GetOrCreate("tag-1")
	if b1 != b2 {
		t.Fatal("同一 tagId 必须复用同一个缓冲")
	}
	b1.Add(time.Now(), 1)

	s.GetOrCreate("tag-2")
	s.Keep(map[string]bool{"tag-1": true})
	if _, ok := s.Get("tag-2"); ok {
		t.Fatal("Keep 应丢弃不在集合中的点位")
	}
	if _, ok := s.Get("tag-1"); !ok {
		t.Fatal("Keep 应保留集合内的点位")
	}

	s.Remove("tag-1")
	if _, ok := s.Get("tag-1"); ok {
		t.Fatal("Remove 应删除缓冲")
	}
}

func TestPointStampIsMillisecond(t *testing.T) {
	// 毫秒时间戳必须落在安全整数范围内，否则经 JSON 传到前端会丢精度
	b := NewBuffer(4)
	b.Add(time.Now(), 1)
	pts := b.Snapshot(0, 0)
	if len(pts) != 1 {
		t.Fatal("应有 1 点")
	}
	const maxSafe = 9007199254740991.0
	if float64(pts[0].T) > maxSafe {
		t.Fatalf("时间戳 %d 超过 JS 安全整数范围", pts[0].T)
	}
	if pts[0].T < 1e12 {
		t.Fatalf("时间戳 %d 不是毫秒级", pts[0].T)
	}
}
