// Package store 历史数据落盘：按点位分目录、按天分片，定长记录追加写。
//
// 选文件分片而不是 SQLite 是为了零依赖（不需要 CGO 与编译器）：
// 每条记录 16 字节 = int64 毫秒时间戳 + float64 数值，小端序。
// 单条 16B 让"按时间二分/顺序扫描"都很直接，读写都不需要解析文本。
package store

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"modbus-hub-go/internal/trend"
)

// RecordSize 单条记录字节数：int64 时间戳 + float64 数值
const RecordSize = 16

// Settings 落库策略
type Settings struct {
	Enabled       bool `json:"enabled"`       // 总开关
	RetentionDays int  `json:"retentionDays"` // 保留天数，0 表示不按时间清理
	MaxSizeMB     int  `json:"maxSizeMB"`     // 容量上限（MB），0 表示不限
}

func (s Settings) Validate() Settings {
	if s.RetentionDays < 0 {
		s.RetentionDays = 0
	}
	if s.RetentionDays > 3650 {
		s.RetentionDays = 3650
	}
	if s.MaxSizeMB < 0 {
		s.MaxSizeMB = 0
	}
	if s.MaxSizeMB > 102400 {
		s.MaxSizeMB = 102400
	}
	return s
}

// Stats 落库的占用情况
type Stats struct {
	Enabled     bool   `json:"enabled"`
	TagCount    int    `json:"tagCount"`    // 有点位目录的数量
	FileCount   int    `json:"fileCount"`   // 分片文件数
	TotalBytes  int64  `json:"totalBytes"`  // 占用字节
	OldestDay   string `json:"oldestDay"`   // 最早分片日期
	NewestDay   string `json:"newestDay"`   // 最新分片日期
	RetentionDays int  `json:"retentionDays"`
	MaxSizeMB    int   `json:"maxSizeMB"`
	Root         string `json:"root"`
}

// Range 某个点位的数据覆盖范围
type Range struct {
	From  int64 `json:"from"`
	To    int64 `json:"to"`
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// History 历史库
type History struct {
	root string

	mu       sync.Mutex
	cfg      Settings
	openFile *os.File
	openKey  string // tagID + "/" + day，用于判断是否需要切换到新分片
	lastPrune time.Time
}

func New(root string, cfg Settings) *History {
	return &History{root: root, cfg: cfg.Validate(), lastPrune: time.Now()}
}

func (h *History) Root() string { return h.root }

func (h *History) Settings() Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg
}

func (h *History) SetSettings(cfg Settings) {
	h.mu.Lock()
	h.cfg = cfg.Validate()
	h.lastPrune = time.Time{} // 下次写入时立刻按新策略清理一次
	h.mu.Unlock()
}

// Append 落一条记录。未启用时直接返回。
func (h *History) Append(tagID string, t time.Time, v float64) error {
	if tagID == "" {
		return nil
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}

	h.mu.Lock()
	if !h.cfg.Enabled {
		h.mu.Unlock()
		return nil
	}
	key := tagID + "/" + dayOf(t)
	if h.openFile == nil || h.openKey != key {
		if err := h.closeLocked(); err != nil {
			h.mu.Unlock()
			return err
		}
		f, err := h.openLocked(tagID, dayOf(t))
		if err != nil {
			h.mu.Unlock()
			return err
		}
		h.openFile = f
		h.openKey = key
	}

	var buf [RecordSize]byte
	binary.LittleEndian.PutUint64(buf[0:8], uint64(t.UnixMilli()))
	binary.LittleEndian.PutUint64(buf[8:16], math.Float64bits(v))
	_, err := h.openFile.Write(buf[:])

	needPrune := time.Since(h.lastPrune) > 10*time.Minute
	h.mu.Unlock()

	if needPrune {
		_, _ = h.Prune()
	}
	return err
}

func (h *History) openLocked(tagID, day string) (*os.File, error) {
	dir := filepath.Join(h.root, safeName(tagID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, day+".dat"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func (h *History) closeLocked() error {
	if h.openFile == nil {
		return nil
	}
	err := h.openFile.Close()
	h.openFile = nil
	h.openKey = ""
	return err
}

// Close 关闭当前打开的分片（进程退出前调用）
func (h *History) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closeLocked()
}

// Query 读某个点位在 [from, to] 毫秒区间内的数据，超量时等间隔降采样。
// to<=0 表示到现在；maxPoints<=0 表示不降采样。
func (h *History) Query(tagID string, from, to int64, maxPoints int) ([]trend.Point, error) {
	if tagID == "" {
		return []trend.Point{}, nil
	}
	if to <= 0 {
		to = time.Now().UnixMilli()
	}
	if from <= 0 {
		from = to - 24*3600*1000 // 默认最近一天
	}
	if from > to {
		from, to = to, from
	}

	days := dayRange(from, to)
	out := make([]trend.Point, 0, 1024)
	for _, day := range days {
		path := filepath.Join(h.root, safeName(tagID), day+".dat")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for off := 0; off+RecordSize <= len(data); off += RecordSize {
			ms := int64(binary.LittleEndian.Uint64(data[off : off+8]))
			if ms < from || ms > to {
				continue
			}
			bits := binary.LittleEndian.Uint64(data[off+8 : off+16])
			out = append(out, trend.Point{T: ms, V: math.Float64frombits(bits)})
		}
	}
	return trend.Downsample(out, maxPoints), nil
}

// Range 扫一遍点位全部数据，给出覆盖的时间范围与采样数
func (h *History) Range(tagID string) (Range, error) {
	r := Range{}
	dir := filepath.Join(h.root, safeName(tagID))
	files, err := listShards(dir)
	if err != nil {
		return r, err
	}
	for _, f := range files {
		path := filepath.Join(dir, f)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		r.Bytes += info.Size()
		r.Count += int(info.Size() / RecordSize)
		data, err := os.ReadFile(path)
		if err != nil || len(data) < RecordSize {
			continue
		}
		first := int64(binary.LittleEndian.Uint64(data[0:8]))
		last := int64(binary.LittleEndian.Uint64(data[len(data)-RecordSize : len(data)-RecordSize+8]))
		if r.From == 0 || first < r.From {
			r.From = first
		}
		if last > r.To {
			r.To = last
		}
	}
	return r, nil
}

// Stats 统计落库占用（扫描全部点位目录）
func (h *History) Stats() Stats {
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()

	st := Stats{
		Enabled:       cfg.Enabled,
		RetentionDays: cfg.RetentionDays,
		MaxSizeMB:     cfg.MaxSizeMB,
		Root:          h.root,
	}
	entries, err := os.ReadDir(h.root)
	if err != nil {
		return st
	}
	oldest, newest := "", ""
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := listShards(filepath.Join(h.root, e.Name()))
		if err != nil || len(files) == 0 {
			continue
		}
		st.TagCount++
		st.FileCount += len(files)
		for _, f := range files {
			info, err := os.Stat(filepath.Join(h.root, e.Name(), f))
			if err != nil {
				continue
			}
			st.TotalBytes += info.Size()
			day := strings.TrimSuffix(f, ".dat")
			if day == "" {
				continue
			}
			if oldest == "" || day < oldest {
				oldest = day
			}
			if newest == "" || day > newest {
				newest = day
			}
		}
	}
	st.OldestDay = oldest
	st.NewestDay = newest
	return st
}

// Prune 滚动清理：先按保留天数删旧分片，再按容量上限从最旧的开始删。
// 返回删除的文件数与释放的字节数。
func (h *History) Prune() (int, int64) {
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()

	h.mu.Lock()
	_ = h.closeLocked() // 清理可能删掉正在写的分片，先关掉句柄
	h.mu.Unlock()

	removed, freed := 0, int64(0)
	if cfg.RetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays).Format("2006-01-02")
		files, _ := h.allShards()
		for _, f := range files {
			day := strings.TrimSuffix(filepath.Base(f), ".dat")
			if day != "" && day < cutoff {
				if info, err := os.Stat(f); err == nil {
					freed += info.Size()
				}
				if err := os.Remove(f); err == nil {
					removed++
				}
			}
		}
	}
	if cfg.MaxSizeMB > 0 {
		limit := int64(cfg.MaxSizeMB) * 1024 * 1024
		files, _ := h.allShards()
		total := int64(0)
		sizes := make(map[string]int64, len(files))
		for _, f := range files {
			if info, err := os.Stat(f); err == nil {
				sizes[f] = info.Size()
				total += info.Size()
			}
		}
		for _, f := range files {
			if total <= limit {
				break
			}
			size := sizes[f]
			if err := os.Remove(f); err != nil {
				continue
			}
			total -= size
			freed += size
			removed++
		}
	}

	h.mu.Lock()
	h.lastPrune = time.Now()
	h.mu.Unlock()
	return removed, freed
}

// Clear 删除某个点位的全部历史
func (h *History) Clear(tagID string) error {
	h.mu.Lock()
	if strings.HasPrefix(h.openKey, tagID+"/") {
		_ = h.closeLocked()
	}
	h.mu.Unlock()
	return os.RemoveAll(filepath.Join(h.root, safeName(tagID)))
}

// ClearAll 清空整个历史库
func (h *History) ClearAll() error {
	h.mu.Lock()
	_ = h.closeLocked()
	h.mu.Unlock()
	if err := os.RemoveAll(h.root); err != nil {
		return err
	}
	return os.MkdirAll(h.root, 0o755)
}

// Forget 点位被删除后一并清掉它的历史
func (h *History) Forget(tagIDs ...string) {
	for _, id := range tagIDs {
		_ = h.Clear(id)
	}
}

// allShards 列出全部分片文件，按 日期 升序（跨点位）
func (h *History) allShards() ([]string, error) {
	entries, err := os.ReadDir(h.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := listShards(filepath.Join(h.root, e.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			out = append(out, filepath.Join(h.root, e.Name(), f))
		}
	}
	// 分片名就是日期，字典序即时间序，清理时从最旧的开始
	sort.Slice(out, func(i, j int) bool {
		return filepath.Base(out[i]) < filepath.Base(out[j])
	})
	return out, nil
}

func listShards(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".dat") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func dayOf(t time.Time) string {
	return t.Format("2006-01-02")
}

func dayRange(from, to int64) []string {
	start := time.UnixMilli(from)
	end := time.UnixMilli(to)
	// 必须按"自然日"迭代：直接对起始时刻加一天会带上时分秒，
	// 跨天时第二天 23:59:50 这种时刻会被判成超过结束时间而漏掉当天分片。
	d := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	last := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	var out []string
	for ; !d.After(last); d = d.AddDate(0, 0, 1) {
		out = append(out, dayOf(d))
	}
	// 跨天最多按 3660 天封顶，避免传错参数时循环过久
	if len(out) > 3660 {
		out = out[:3660]
	}
	return out
}

// safeName 点位 Id 是 UUID，理论上安全；仍过滤一次防止配置被手改成路径
func safeName(id string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	s := r.Replace(strings.TrimSpace(id))
	if s == "" {
		return "_" + strconv.Itoa(int(time.Now().UnixNano()))
	}
	return s
}
