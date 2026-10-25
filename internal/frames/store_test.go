package frames

import (
	"strings"
	"testing"
	"time"

	"modbus-hub-go/internal/modbus"
)

func rec(ch, dir, hex, summary string, level Level) Record {
	return Record{
		Time:        "10:00:00.000",
		ChannelID:   ch,
		ChannelName: "通道" + ch,
		Direction:   dir,
		Hex:         hex,
		Summary:     summary,
		Level:       string(level),
	}
}

func TestAppendAndList(t *testing.T) {
	s := New(10)
	s.Append(rec("a", string(modbus.FrameTx), "01 03", "读保持寄存器", LevelOK))
	s.Append(rec("a", string(modbus.FrameRx), "01 03 04", "返回 2 个寄存器", LevelOK))
	s.Append(rec("b", string(modbus.FrameTx), "02 06", "写单个寄存器", LevelOK))

	all := s.List("", "", "")
	if len(all) != 3 {
		t.Fatalf("应查到 3 条，实际 %d", len(all))
	}
	// 列表按时间正序，序号也要递增，界面靠它定位
	if all[0].Seq != 1 || all[2].Seq != 3 {
		t.Fatalf("序号应递增：%+v", all)
	}

	if got := s.List("b", "", ""); len(got) != 1 || got[0].ChannelID != "b" {
		t.Fatalf("按通道过滤不对：%+v", got)
	}
	if got := s.List("", string(modbus.FrameRx), ""); len(got) != 1 || got[0].Direction != "rx" {
		t.Fatalf("按方向过滤不对：%+v", got)
	}
	if got := s.List("a", string(modbus.FrameTx), ""); len(got) != 1 {
		t.Fatalf("通道+方向组合过滤不对：%+v", got)
	}
}

// 关键字同时匹配十六进制与摘要，且大小写不敏感
func TestListKeyword(t *testing.T) {
	s := New(10)
	s.Append(rec("a", "tx", "01 03 00 00", "读保持寄存器 起始 0 数量 2", LevelOK))
	s.Append(rec("a", "rx", "01 83 02", "异常响应", LevelError))

	if got := s.List("", "", "01 03"); len(got) != 1 {
		t.Fatalf("按十六进制过滤应命中 1 条，实际 %d", len(got))
	}
	if got := s.List("", "", "异常"); len(got) != 1 {
		t.Fatalf("按摘要过滤应命中 1 条，实际 %d", len(got))
	}
	if got := s.List("", "", "01 03 00 00"); len(got) != 1 {
		t.Fatalf("十六进制带空格也要能匹配，实际 %d", len(got))
	}
	if got := s.List("", "", "不存在的关键字"); len(got) != 0 {
		t.Fatalf("无匹配应返回空，实际 %d", len(got))
	}
}

// 缓冲写满后覆盖最旧的，不能无限涨
func TestRingOverwritesOldest(t *testing.T) {
	s := New(3)
	for i := 0; i < 5; i++ {
		s.Append(rec("a", "tx", "01", "第几条", LevelOK))
	}
	list := s.List("", "", "")
	if len(list) != 3 {
		t.Fatalf("容量 3 的缓冲应只留 3 条，实际 %d", len(list))
	}
	// 留下的应是最后三条，序号 3/4/5
	if list[0].Seq != 3 || list[2].Seq != 5 {
		t.Fatalf("应保留最新的三条，实际 %+v", list)
	}
	if st := s.Stats(); st.Dropped != 2 {
		t.Fatalf("应记录丢弃 2 条，实际 %d", st.Dropped)
	}
	if st := s.Stats(); st.Total != 3 || st.Capacity != 3 {
		t.Fatalf("统计不对：%+v", st)
	}
}

// 覆盖时收发与异常计数要跟着减，否则数字会越跑越离谱
func TestStatsCountRecycle(t *testing.T) {
	s := New(2)
	s.Append(rec("a", "tx", "01", "", LevelOK))
	s.Append(rec("a", "rx", "02", "", LevelError))
	st := s.Stats()
	if st.Tx != 1 || st.Rx != 1 || st.Errors != 1 {
		t.Fatalf("初始统计不对：%+v", st)
	}
	// 再写两条，把上面两条都挤掉
	s.Append(rec("a", "tx", "03", "", LevelOK))
	s.Append(rec("a", "tx", "04", "", LevelOK))
	st = s.Stats()
	if st.Tx != 2 || st.Rx != 0 || st.Errors != 0 {
		t.Fatalf("覆盖后统计应重新平衡，实际 %+v", st)
	}
}

// 清除单个通道不能把别的通道一起清了
func TestClearByChannel(t *testing.T) {
	s := New(10)
	s.Append(rec("a", "tx", "01", "", LevelOK))
	s.Append(rec("b", "rx", "02", "", LevelError))
	s.Append(rec("a", "rx", "03", "", LevelOK))
	s.Append(rec("b", "tx", "04", "", LevelOK))

	s.Clear("a")
	rest := s.List("", "", "")
	if len(rest) != 2 {
		t.Fatalf("应只剩 b 通道的 2 条，实际 %d", len(rest))
	}
	for _, r := range rest {
		if r.ChannelID != "b" {
			t.Fatalf("清错了通道：%+v", r)
		}
	}
	st := s.Stats()
	if st.Tx != 1 || st.Rx != 1 || st.Errors != 1 {
		t.Fatalf("清通道后统计不对：%+v", st)
	}

	s.Clear("")
	if len(s.List("", "", "")) != 0 {
		t.Fatal("全清后应为空")
	}
	if st := s.Stats(); st.Total != 0 || st.Dropped != 0 || st.Errors != 0 {
		t.Fatalf("全清后统计应归零：%+v", st)
	}
}

func TestGetDetail(t *testing.T) {
	s := New(10)
	s.Append(rec("a", "tx", "01 03", "摘要", LevelOK))
	r, ok := s.Get(1)
	if !ok {
		t.Fatal("应能按序号取到")
	}
	if r.Hex != "01 03" {
		t.Fatalf("取到的报文不对：%q", r.Hex)
	}
	if _, ok := s.Get(999); ok {
		t.Fatal("不存在的序号应返回 false")
	}
}

// 缓冲被覆盖后按旧序号取不到，界面要给出"已被覆盖"的提示而不是显示空白
func TestGetDetailAfterOverwrite(t *testing.T) {
	s := New(2)
	s.Append(rec("a", "tx", "01", "", LevelOK))
	s.Append(rec("a", "tx", "02", "", LevelOK))
	s.Append(rec("a", "tx", "03", "", LevelOK))
	if _, ok := s.Get(1); ok {
		t.Fatal("被覆盖的序号不应再取到")
	}
	if _, ok := s.Get(3); !ok {
		t.Fatal("仍在缓冲里的序号应取得到")
	}
}

// 真实帧走一遍完整链路：解析结果进缓冲后，界面拿到的字段要齐全。
// CRC 现场算出来，不抄硬编码常量——抄错了用例会假装通过。
func TestRecordFromRealFrame(t *testing.T) {
	body := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x02}
	crc := modbus.CRC16(body)
	frame := append(append([]byte{}, body...), byte(crc&0xFF), byte(crc>>8))

	info := modbus.DescribeFrame(frame, modbus.FrameTx)
	if !info.CRCOK {
		t.Fatal("现场算出的 CRC 应校验通过")
	}
	r := Record{
		Time:        modbus.FrameStamp(time.Date(2026, 10, 25, 9, 30, 15, 250000000, time.Local)),
		ChannelID:   "ch1",
		ChannelName: "1号通道",
		Direction:   string(modbus.FrameTx),
		Slave:       info.Slave,
		FuncCode:    info.FuncCode,
		Length:      len(frame),
		Hex:         modbus.FrameHex(frame),
		ASCII:       modbus.FrameASCII(frame),
		Summary:     info.Summary,
		Level:       string(LevelOK),
		Fields:      info.Fields,
	}

	s := New(4)
	s.Append(r)
	got := s.List("ch1", "", "")
	if len(got) != 1 {
		t.Fatalf("应查到 1 条，实际 %d", len(got))
	}
	if got[0].Slave != 1 || got[0].FuncCode != 3 {
		t.Fatalf("从机与功能码不对：%+v", got[0])
	}
	if !strings.HasPrefix(got[0].Hex, "01 03 00 00 00 02") {
		t.Fatalf("十六进制不对：%q", got[0].Hex)
	}
	if got[0].Length != 8 {
		t.Fatalf("字节数应为 8，实际 %d", got[0].Length)
	}
	if got[0].Time != "09:30:15.250" {
		t.Fatalf("时间戳不对：%q", got[0].Time)
	}
}
