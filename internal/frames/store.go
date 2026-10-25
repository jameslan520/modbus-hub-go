// Package frames 通信报文的记录与查询。
//
// 报文是排查现场问题时最直接的一手材料，但也是最容易把内存吃干的东西：
// 一个 100 点、1 秒周期的通道，一收一发就是每秒 200 帧，
// 十分钟就是 12 万条。所以这里用定长环形缓冲，写满就覆盖最旧的，
// 界面上要翻旧数据就导 CSV。
package frames

import (
	"strings"
	"sync"

	"modbus-hub-go/internal/modbus"
)

// DefaultCapacity 默认保留的报文条数。
// 2000 条大约够看 10~20 帧/秒的通道两分钟，排查问题足够，
// 又不至于让内存无上限增长。
const DefaultCapacity = 2000

// Level 报文的严重程度，界面用来染色
type Level string

const (
	// LevelOK 正常收发
	LevelOK Level = "ok"
	// LevelWarn 收到了但有问题（CRC 错、地址/功能码不符），会重试
	LevelWarn Level = "warn"
	// LevelError 异常响应、超时、发送失败——这一轮请求没成
	LevelError Level = "error"
)

// Record 一条报文记录。前端直接拿去渲染，字段命名保持与界面一致。
type Record struct {
	Seq         int64  `json:"seq"`         // 全局递增序号，界面用来定位与去重
	Time        string `json:"time"`        // HH:MM:SS.mmm
	TimeMs      int64  `json:"timeMs"`      // 毫秒时间戳，导出与排序用
	ChannelID   string `json:"channelId"`   // 所属通道，空串表示未归属
	ChannelName string `json:"channelName"` // 通道名，界面显示
	Direction   string `json:"direction"`   // tx 发送 / rx 接收
	Protocol    string `json:"protocol"`    // RTU / TCP，展开详情时说明这一帧来自哪种链路
	Slave       int    `json:"slave"`       // 从机地址，解析不出为 -1
	FuncCode    int    `json:"funcCode"`    // 功能码，解析不出为 -1
	Length      int    `json:"length"`      // 帧字节数
	Hex         string `json:"hex"`         // "01 03 00 00 00 02 C4 0B"
	ASCII       string `json:"ascii"`       // 可打印字符，其余用点占位
	Summary     string `json:"summary"`     // 一句话摘要
	Note        string `json:"note"`        // 补充说明：超时、CRC 错、异常原因等
	Level       string `json:"level"`       // ok / warn / error
	// Fields 逐字段明细，展开行时显示；不进列表查询的序列化以省流量
	Fields []modbus.FrameField `json:"fields,omitempty"`
}

// Stats 缓冲区概况
type Stats struct {
	Total    int `json:"total"`    // 缓冲里的条数
	Dropped  int `json:"dropped"`  // 自上次清空以来被覆盖掉的条数
	Capacity int `json:"capacity"` // 缓冲容量
	Tx       int `json:"tx"`       // 缓冲里的发送条数
	Rx       int `json:"rx"`       // 缓冲里的接收条数
	Errors   int `json:"errors"`   // 缓冲里的异常条数
}

// Store 定长环形缓冲，线程安全
type Store struct {
	mu       sync.Mutex
	buf      []Record
	capacity int
	next     int // 下一个写入位置
	size     int // 当前条数
	seq      int64
	dropped  int64
	tx       int
	rx       int
	errs     int
}

func New(capacity int) *Store {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Store{buf: make([]Record, capacity), capacity: capacity}
}

// Append 写入一条报文；缓冲满了就覆盖最旧的，并累加丢弃计数。
// 计数只在内部维护，界面上"已丢弃 N 条"能提醒用户别指望翻太久以前的。
func (s *Store) Append(r Record) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	r.Seq = s.seq

	if s.size == s.capacity {
		old := s.buf[s.next]
		s.countRemoved(old)
		s.dropped++
	} else {
		s.size++
	}
	s.buf[s.next] = r
	s.next = (s.next + 1) % s.capacity

	s.countAdded(r)
}

func (s *Store) countAdded(r Record) {
	if r.Direction == string(modbus.FrameTx) {
		s.tx++
	} else {
		s.rx++
	}
	if r.Level == string(LevelError) {
		s.errs++
	}
}

func (s *Store) countRemoved(r Record) {
	if r.Direction == string(modbus.FrameTx) {
		s.tx--
	} else {
		s.rx--
	}
	if r.Level == string(LevelError) {
		s.errs--
	}
}

// List 按条件查询，返回按时间正序的记录（最旧的在前）。
// channelID 为空表示所有通道；dir 为空表示收发都要；
// keyword 同时匹配十六进制与摘要，大小写不敏感。
func (s *Store) List(channelID, dir, keyword string) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()

	kw := normalizeKeyword(keyword)
	out := make([]Record, 0, s.size)
	start := (s.next - s.size + s.capacity) % s.capacity
	for i := 0; i < s.size; i++ {
		r := s.buf[(start+i)%s.capacity]
		if channelID != "" && r.ChannelID != channelID {
			continue
		}
		if dir != "" && r.Direction != dir {
			continue
		}
		if kw != "" && !matchKeyword(r, kw) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func normalizeKeyword(kw string) string {
	return strings.ToUpper(strings.TrimSpace(kw))
}

func matchKeyword(r Record, kw string) bool {
	return strings.Contains(strings.ToUpper(r.Hex), kw) ||
		strings.Contains(strings.ToUpper(r.Summary), kw) ||
		strings.Contains(strings.ToUpper(r.Note), kw)
}

// Get 按序号取一条，用于展开详情时拿到完整字段
func (s *Store) Get(seq int64) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := (s.next - s.size + s.capacity) % s.capacity
	for i := 0; i < s.size; i++ {
		r := s.buf[(start+i)%s.capacity]
		if r.Seq == seq {
			return r, true
		}
	}
	return Record{}, false
}

// Clear 清空。channelID 为空时清空全部，否则只清这个通道的——
// 清单个通道不能简单置零，得把剩下的重新排一遍。
func (s *Store) Clear(channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if channelID == "" {
		s.buf = make([]Record, s.capacity)
		s.next = 0
		s.size = 0
		s.tx = 0
		s.rx = 0
		s.errs = 0
		s.dropped = 0
		return
	}

	kept := make([]Record, 0, s.size)
	start := (s.next - s.size + s.capacity) % s.capacity
	for i := 0; i < s.size; i++ {
		r := s.buf[(start+i)%s.capacity]
		if r.ChannelID != channelID {
			kept = append(kept, r)
		} else {
			s.dropped++
		}
	}
	s.buf = make([]Record, s.capacity)
	copy(s.buf, kept)
	s.next = len(kept) % s.capacity
	s.size = len(kept)
	s.recount()
}

func (s *Store) recount() {
	s.tx, s.rx, s.errs = 0, 0, 0
	for i := 0; i < s.size; i++ {
		s.countAdded(s.buf[i])
	}
}

// Stats 缓冲区概况
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		Total:    s.size,
		Dropped:  int(s.dropped),
		Capacity: s.capacity,
		Tx:       s.tx,
		Rx:       s.rx,
		Errors:   s.errs,
	}
}

// ClearDropped 把"已丢弃"计数归零。界面切换过滤条件时调用，
// 免得那个数字一直挂着，看着像刚丢了很多。
func (s *Store) ClearDropped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropped = 0
}
