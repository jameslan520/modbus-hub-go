package hub

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"modbus-hub-go/internal/alarm"
	"modbus-hub-go/internal/modbus"
	"modbus-hub-go/internal/models"
)

// Callbacks 通道服务向外部（UI）抛事件
type Callbacks struct {
	OnLog    func(models.LogEntry)
	OnValues func(channelID string, values []models.TagValue)
	OnState  func(channelID string, state models.ChannelState)
	OnAlarm  func(alarm.Event)
}

// ChannelService 单个串口通道的轮询与采集
type ChannelService struct {
	cb Callbacks

	mu      sync.Mutex
	cfg     models.ChannelConfig
	client  *modbus.RTUClient
	stopCh  chan struct{}
	wg      sync.WaitGroup
	state   models.ChannelState
	values  map[string]models.TagValue
	lastErr map[string]string
	lastTx  int64
	lastRx  int64
}

func NewChannelService(cfg models.ChannelConfig, cb Callbacks) *ChannelService {
	s := &ChannelService{
		cfg:     cfg,
		state:   models.StateDisconnected,
		values:  make(map[string]models.TagValue),
		lastErr: make(map[string]string),
		cb:      cb,
	}
	for _, t := range cfg.Tags {
		s.values[t.ID] = models.TagValue{
			TagID:        t.ID,
			Quality:      "Bad",
			DisplayValue: "--",
			Timestamp:    models.Now(),
		}
	}
	return s
}

func (s *ChannelService) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.ID
}

func (s *ChannelService) State() models.ChannelState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *ChannelService) Traffic() (int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client.TxCount(), s.client.RxCount()
	}
	return s.lastTx, s.lastRx
}

func (s *ChannelService) Config() models.ChannelConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// SetConfig 更新配置（串口参数变化由 Hub 负责重启）
func (s *ChannelService) SetConfig(cfg models.ChannelConfig) {
	s.mu.Lock()
	s.cfg = cfg
	for _, t := range cfg.Tags {
		if _, ok := s.values[t.ID]; !ok {
			s.values[t.ID] = models.TagValue{
				TagID:        t.ID,
				Quality:      "Bad",
				DisplayValue: "--",
				Timestamp:    models.Now(),
			}
		}
	}
	s.mu.Unlock()
}

func (s *ChannelService) Values() []models.TagValue {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.TagValue, 0, len(s.cfg.Tags))
	for _, t := range s.cfg.Tags {
		if v, ok := s.values[t.ID]; ok {
			out = append(out, v)
			continue
		}
		out = append(out, models.TagValue{TagID: t.ID, Quality: "Bad", DisplayValue: "--"})
	}
	return out
}

// Start 打开串口并开始轮询
func (s *ChannelService) Start() error {
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return nil
	}
	s.state = models.StateConnecting
	cfg := s.cfg
	s.mu.Unlock()
	s.emitState()
	s.log("Info", fmt.Sprintf("正在连接 %s...", cfg.PortName))

	client := modbus.NewRTUClient(cfg)
	if err := client.Connect(); err != nil {
		s.mu.Lock()
		s.state = models.StateError
		s.mu.Unlock()
		s.emitState()
		s.log("Error", "连接失败: "+err.Error())
		return err
	}

	stop := make(chan struct{})
	s.mu.Lock()
	s.client = client
	s.stopCh = stop
	s.state = models.StateConnected
	s.mu.Unlock()
	s.emitState()
	s.log("Info", fmt.Sprintf("通道已连接: %s @ %d", cfg.PortName, cfg.BaudRate))

	s.wg.Add(1)
	go s.pollLoop(stop)
	return nil
}

// Stop 停止轮询并关闭串口
func (s *ChannelService) Stop() error {
	s.mu.Lock()
	client := s.client
	stop := s.stopCh
	s.client = nil
	s.stopCh = nil
	if client != nil {
		s.lastTx = client.TxCount()
		s.lastRx = client.RxCount()
	}
	s.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	s.wg.Wait()
	if client != nil {
		_ = client.Close()
	}

	s.mu.Lock()
	s.state = models.StateDisconnected
	s.mu.Unlock()
	s.emitState()
	s.log("Info", "通道已停止")
	return nil
}

func (s *ChannelService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client != nil
}

func (s *ChannelService) pollLoop(stop <-chan struct{}) {
	defer s.wg.Done()
	for {
		s.pollOnce()
		interval := 1000
		s.mu.Lock()
		if s.cfg.PollingIntervalMs > 0 {
			interval = s.cfg.PollingIntervalMs
		}
		s.mu.Unlock()
		if interval < 100 {
			interval = 100
		}
		select {
		case <-stop:
			return
		case <-time.After(time.Duration(interval) * time.Millisecond):
		}
	}
}

func (s *ChannelService) pollOnce() {
	s.mu.Lock()
	client := s.client
	cfg := s.cfg
	s.mu.Unlock()
	if client == nil {
		return
	}

	enabled := make([]models.TagPoint, 0, len(cfg.Tags))
	for _, t := range cfg.Tags {
		if t.IsEnabled {
			enabled = append(enabled, t)
		}
	}
	if len(enabled) == 0 {
		return
	}

	values := client.ReadTags(enabled)
	s.mu.Lock()
	for _, v := range values {
		s.values[v.TagID] = v
	}
	s.mu.Unlock()

	if s.cb.OnValues != nil {
		s.cb.OnValues(cfg.ID, values)
	}
	s.reportQuality(enabled, values)
}

func (s *ChannelService) reportQuality(tags []models.TagPoint, values []models.TagValue) {
	bad := make([]models.TagValue, 0, len(values))
	for _, v := range values {
		if v.Quality == "Bad" && v.ErrorMessage != "" {
			bad = append(bad, v)
		}
	}
	if len(bad) == 0 {
		s.lastErr = make(map[string]string)
		return
	}

	if len(bad) == len(values) {
		msg := bad[0].ErrorMessage
		if s.lastErr["__block__"] != msg {
			s.lastErr["__block__"] = msg
			s.log("Warn", "块读取失败: "+msg)
		}
		return
	}

	delete(s.lastErr, "__block__")
	for _, v := range values {
		if v.Quality == "Bad" && v.ErrorMessage != "" {
			if s.lastErr[v.TagID] != v.ErrorMessage {
				s.lastErr[v.TagID] = v.ErrorMessage
				name := v.TagID
				for _, t := range tags {
					if t.ID == v.TagID {
						name = t.Name
						break
					}
				}
				s.log("Warn", fmt.Sprintf("%s: %s", name, v.ErrorMessage))
			}
		} else {
			delete(s.lastErr, v.TagID)
		}
	}
}

// WriteTag 单点写入（写后回读）
func (s *ChannelService) WriteTag(tagID, value string) error {
	s.mu.Lock()
	client := s.client
	cfg := s.cfg
	s.mu.Unlock()
	if client == nil {
		return errors.New("通道未连接")
	}

	var tag models.TagPoint
	found := false
	for _, t := range cfg.Tags {
		if t.ID == tagID {
			tag = t
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("未找到标签: %s", tagID)
	}
	if !tag.IsWritable() {
		return fmt.Errorf("标签 %s 不可写", tag.Name)
	}

	raw, err := modbus.ParseInput(tag, value)
	if err != nil {
		return fmt.Errorf("%s：%w", tag.Name, err)
	}
	if err := client.WriteTag(tag, raw); err != nil {
		return err
	}
	s.log("Info", fmt.Sprintf("写入 %s = %s", tag.Name, value))
	s.refresh([]models.TagPoint{tag})
	return nil
}

// WriteTags 功能码 16 连续批量写入（写后回读）
func (s *ChannelService) WriteTags(items []WriteTagRequest) error {
	s.mu.Lock()
	client := s.client
	cfg := s.cfg
	s.mu.Unlock()
	if client == nil {
		return errors.New("通道未连接")
	}
	if len(items) == 0 {
		return nil
	}

	writeItems := make([]modbus.WriteItem, 0, len(items))
	tags := make([]models.TagPoint, 0, len(items))
	for _, req := range items {
		tag, ok := findTag(cfg, req.TagID)
		if !ok {
			return fmt.Errorf("未找到标签: %s", req.TagID)
		}
		writeItems = append(writeItems, modbus.WriteItem{Tag: tag, Value: req.Value})
		tags = append(tags, tag)
	}

	if err := modbus.EnsureWritableConsecutive(tags); err != nil {
		return err
	}

	for _, chunk := range modbus.SplitByPduLimit(writeItems) {
		regs, err := modbus.EncodeBlock(chunk)
		if err != nil {
			return err
		}
		start := modbus.OrderByAddress(itemTags(chunk))[0].Address
		if err := client.WriteMultipleRegisters(chunk[0].Tag.SlaveID, start, regs); err != nil {
			return err
		}
		var names []string
		for _, it := range chunk {
			names = append(names, fmt.Sprintf("%s=%s", it.Tag.Name, it.Value))
		}
		s.log("Info", fmt.Sprintf("功能码 16 连续写入 %d 点 @%d：%s", len(chunk), start, joinNames(names)))
	}

	s.refresh(tags)
	return nil
}

// WriteRegisters 直接写一段保持寄存器（标定下载用）
func (s *ChannelService) WriteRegisters(start uint16, regs []uint16) error {
	s.mu.Lock()
	client := s.client
	cfg := s.cfg
	s.mu.Unlock()
	if client == nil {
		return errors.New("通道未运行，请先启动通道再下载标定")
	}
	if len(regs) == 0 {
		return nil
	}
	if int(start)+len(regs) > 65536 {
		return errors.New("写入范围超出保持寄存器地址上限 65535")
	}

	offset := 0
	for offset < len(regs) {
		count := min(modbus.MaxWriteRegisters, len(regs)-offset)
		addr := uint16(int(start) + offset)
		if err := client.WriteMultipleRegisters(cfg.SlaveID, addr, regs[offset:offset+count]); err != nil {
			return err
		}
		s.log("Info", fmt.Sprintf("功能码 16 写入 %d 个寄存器 @%d", count, addr))
		offset += count
	}
	return nil
}

func (s *ChannelService) refresh(tags []models.TagPoint) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return
	}
	values := client.ReadTags(tags)
	s.mu.Lock()
	for _, v := range values {
		s.values[v.TagID] = v
	}
	s.mu.Unlock()
	if s.cb.OnValues != nil {
		s.cb.OnValues(s.cfg.ID, values)
	}
}

func (s *ChannelService) log(level, message string) {
	if s.cb.OnLog == nil {
		return
	}
	s.mu.Lock()
	name := s.cfg.Name
	s.mu.Unlock()
	s.cb.OnLog(models.LogEntry{
		Timestamp:   models.Now(),
		Level:       level,
		ChannelName: name,
		Message:     message,
	})
}

func (s *ChannelService) emitState() {
	if s.cb.OnState == nil {
		return
	}
	s.mu.Lock()
	id := s.cfg.ID
	state := s.state
	s.mu.Unlock()
	s.cb.OnState(id, state)
}

func findTag(cfg models.ChannelConfig, tagID string) (models.TagPoint, bool) {
	for _, t := range cfg.Tags {
		if t.ID == tagID {
			return t, true
		}
	}
	return models.TagPoint{}, false
}

func itemTags(items []modbus.WriteItem) []models.TagPoint {
	out := make([]models.TagPoint, 0, len(items))
	for _, it := range items {
		out = append(out, it.Tag)
	}
	return out
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "、"
		}
		out += n
	}
	return out
}
