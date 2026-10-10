package trend

import (
	"math"
	"sync"
	"time"
)

// DefaultCapacity 每个点位保留的采样点数。
// 1000ms 轮询下约 4.5 小时，200ms 下约 55 分钟。
const DefaultCapacity = 16384

// Point 一个采样点。
// 时间戳用毫秒而非纳秒：纳秒值约 1.7e18，超过 JS Number.MAX_SAFE_INTEGER(9e15)，
// 经 JSON 传到前端会丢精度。毫秒足够曲线使用且能安全放进 JS number。
type Point struct {
	T int64   `json:"t"` // Unix 毫秒
	V float64 `json:"v"`
}

// Stats 时间窗口内的统计
type Stats struct {
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Avg   float64 `json:"avg"`
	Last  float64 `json:"last"`
	Count int     `json:"count"`
}

// Buffer 定长环形缓冲，按时间顺序保存点位数值。
type Buffer struct {
	mu       sync.Mutex
	capacity int
	values   []float64
	stamps   []int64
	head     int
	count    int
	version  uint64
}

func NewBuffer(capacity int) *Buffer {
	if capacity < 32 {
		capacity = 32
	}
	return &Buffer{
		capacity: capacity,
		values:   make([]float64, capacity),
		stamps:   make([]int64, capacity),
	}
}

func (b *Buffer) Capacity() int { return b.capacity }

func (b *Buffer) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// Version 每次写入递增，前端可用它判断是否需要重绘
func (b *Buffer) Version() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.version
}

// Add 追加一个采样点；NaN/Inf 直接丢弃，避免污染曲线与统计
func (b *Buffer) Add(t time.Time, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	ms := t.UnixMilli()

	b.mu.Lock()
	defer b.mu.Unlock()

	// 同一毫秒且同值的重复写入视为冗余（刷新点表、写后回读会触发）
	if b.count > 0 {
		last := (b.head + b.count - 1) % b.capacity
		if b.stamps[last] == ms && b.values[last] == v {
			return
		}
	}

	var write int
	if b.count < b.capacity {
		write = (b.head + b.count) % b.capacity
		b.count++
	} else {
		write = b.head
		b.head = (b.head + 1) % b.capacity
	}
	b.values[write] = v
	b.stamps[write] = ms
	b.version++
}

// collectLocked 按时间正序取出窗口内的点（调用方持有锁）
func (b *Buffer) collectLocked(window time.Duration) []Point {
	if b.count == 0 {
		return nil
	}
	cutoff := int64(0)
	if window > 0 {
		last := (b.head + b.count - 1) % b.capacity
		cutoff = b.stamps[last] - window.Milliseconds()
	}

	out := make([]Point, 0, b.count)
	for i := 0; i < b.count; i++ {
		idx := (b.head + i) % b.capacity
		if b.stamps[idx] < cutoff {
			continue
		}
		out = append(out, Point{T: b.stamps[idx], V: b.values[idx]})
	}
	return out
}

// Snapshot 取窗口内的点，超过 maxPoints 时等间隔降采样（保留首尾）
func (b *Buffer) Snapshot(window time.Duration, maxPoints int) []Point {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Downsample(b.collectLocked(window), maxPoints)
}

// Recent 取最近 n 个点（不降采样），标定的采样均值用它
func (b *Buffer) Recent(n int) []Point {
	b.mu.Lock()
	defer b.mu.Unlock()
	pts := b.collectLocked(0)
	if n > 0 && len(pts) > n {
		return pts[len(pts)-n:]
	}
	return pts
}

// Stats 计算窗口内的最小值 / 最大值 / 平均值 / 末值
func (b *Buffer) Stats(window time.Duration) (Stats, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	pts := b.collectLocked(window)
	if len(pts) == 0 {
		return Stats{}, false
	}

	st := Stats{Min: pts[0].V, Max: pts[0].V, Last: pts[len(pts)-1].V}
	sum := 0.0
	for _, p := range pts {
		if p.V < st.Min {
			st.Min = p.V
		}
		if p.V > st.Max {
			st.Max = p.V
		}
		sum += p.V
	}
	st.Avg = sum / float64(len(pts))
	st.Count = len(pts)
	return st, true
}

// Clear 清空缓冲
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.count = 0
	b.version++
}

// Downsample 等间隔抽样，保证首尾点都在。
// maxPoints<=0 表示不降采样（导出全量时用）；maxPoints==1 只保留末点。
func Downsample(pts []Point, maxPoints int) []Point {
	if len(pts) == 0 {
		return []Point{}
	}
	if maxPoints <= 0 {
		return pts
	}
	if maxPoints == 1 {
		return []Point{pts[len(pts)-1]}
	}
	if len(pts) <= maxPoints {
		return pts
	}

	out := make([]Point, 0, maxPoints)
	step := float64(len(pts)-1) / float64(maxPoints-1)
	for i := 0; i < maxPoints; i++ {
		idx := int(float64(i)*step + 0.5)
		if idx >= len(pts) {
			idx = len(pts) - 1
		}
		out = append(out, pts[idx])
	}
	return out
}
