package trend

import (
	"sync"
	"time"
)

// Store 按点位 Id 复用历史缓冲，避免切换通道或刷新点表时曲线被清空
type Store struct {
	mu      sync.Mutex
	buffers map[string]*Buffer
}

func NewStore() *Store {
	return &Store{buffers: make(map[string]*Buffer)}
}

func (s *Store) GetOrCreate(tagID string) *Buffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buffers[tagID]
	if !ok {
		b = NewBuffer(DefaultCapacity)
		s.buffers[tagID] = b
	}
	return b
}

func (s *Store) Get(tagID string) (*Buffer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buffers[tagID]
	return b, ok
}

// Remove 删除单个点位的缓冲（删除点位或清空曲线时调用）
func (s *Store) Remove(tagID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buffers, tagID)
}

// Keep 只保留 keep 中的点位，其余丢弃（删除通道、保存配置后清理）
func (s *Store) Keep(keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.buffers {
		if !keep[id] {
			delete(s.buffers, id)
		}
	}
}

func (s *Store) Snapshot(tagID string, window time.Duration, maxPoints int) []Point {
	b, ok := s.Get(tagID)
	if !ok {
		return []Point{}
	}
	return b.Snapshot(window, maxPoints)
}

func (s *Store) Stats(tagID string, window time.Duration) (Stats, bool) {
	b, ok := s.Get(tagID)
	if !ok {
		return Stats{}, false
	}
	return b.Stats(window)
}
