package hub

import (
	"strings"
	"testing"

	"modbus-hub-go/internal/models"
)

func silentCallbacks() Callbacks {
	return Callbacks{
		OnLog:    func(models.LogEntry) {},
		OnValues: func(string, []models.TagValue) {},
		OnState:  func(string, models.ChannelState) {},
	}
}

// 通道配置保存后应能被再次读出，且非法的串口参数不会 panic
func TestSaveAndReload(t *testing.T) {
	h := New(silentCallbacks())
	cfg := models.NewChannelConfig("测试通道")
	cfg.PortName = "COM99"
	cfg.Parity = models.ParityOdd
	cfg.StopBits = models.StopBitsTwo
	cfg.PollingIntervalMs = 500
	cfg.SlaveID = 7
	cfg.Tags = []models.TagPoint{models.NewTagPoint("温度")}
	if err := h.SaveChannels([]models.ChannelConfig{cfg}); err != nil {
		t.Fatalf("保存失败：%v", err)
	}

	again := New(silentCallbacks()).Channels()
	if len(again) != 1 {
		t.Fatalf("重新加载应得到 1 个通道，得到 %d", len(again))
	}
	got := again[0]
	if got.PortName != "COM99" || got.Parity != models.ParityOdd || got.StopBits != models.StopBitsTwo {
		t.Fatalf("串口参数未正确持久化：%+v", got)
	}
	if got.SlaveID != 7 {
		t.Fatalf("从机号未持久化：%d", got.SlaveID)
	}
}

// 打开不存在的串口应返回错误而不是 panic
func TestStartInvalidPort(t *testing.T) {
	h := New(silentCallbacks())
	cfg := models.NewChannelConfig("无效口")
	cfg.PortName = "COM199"
	cfg.Tags = []models.TagPoint{models.NewTagPoint("t")}
	if err := h.SaveChannels([]models.ChannelConfig{cfg}); err != nil {
		t.Fatalf("保存失败：%v", err)
	}

	err := h.Start(cfg.ID)
	if err == nil {
		t.Fatal("不存在的串口应启动失败")
	}
	if !strings.Contains(err.Error(), "打开串口") {
		t.Fatalf("错误信息应说明打开串口失败，实际：%v", err)
	}

	rt := h.Runtime()
	if len(rt) != 1 {
		t.Fatalf("运行态应有 1 项，得到 %d", len(rt))
	}
	if rt[0].Running {
		t.Fatal("启动失败后不应处于运行状态")
	}
	if rt[0].State != models.StateError {
		t.Fatalf("启动失败后状态应为 Error，实际 %s", rt[0].State)
	}
}

// 禁用通道不允许启动
func TestStartDisabledChannel(t *testing.T) {
	h := New(silentCallbacks())
	cfg := models.NewChannelConfig("禁用通道")
	cfg.IsEnabled = false
	if err := h.SaveChannels([]models.ChannelConfig{cfg}); err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	if err := h.Start(cfg.ID); err == nil {
		t.Fatal("禁用通道应启动失败")
	}
}

// 未运行通道的写入应给出明确提示
func TestWriteOnStoppedChannel(t *testing.T) {
	h := New(silentCallbacks())
	cfg := models.NewChannelConfig("停机通道")
	cfg.Tags = []models.TagPoint{models.NewTagPoint("t")}
	if err := h.SaveChannels([]models.ChannelConfig{cfg}); err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	if err := h.WriteTag(cfg.ID, cfg.Tags[0].ID, "1", models.InputModeRaw); err == nil {
		t.Fatal("通道未运行时写入应报错")
	}
	if err := h.WriteRegisters(cfg.ID, 0, []uint16{1}); err == nil {
		t.Fatal("通道未运行时下载标定应报错")
	}
}
