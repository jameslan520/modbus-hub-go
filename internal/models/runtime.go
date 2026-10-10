package models

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// TagValue 单个点的当前值
type TagValue struct {
	TagID        string   `json:"tagId"`
	RawValue     any      `json:"rawValue"`
	NumericValue *float64 `json:"numericValue"`
	DisplayValue string   `json:"displayValue"`
	Quality      string   `json:"quality"`
	Timestamp    string   `json:"timestamp"`
	// TimestampMs 采集时刻的毫秒时间戳。Timestamp 只到秒，画曲线会阶梯化，
	// 曲线与趋势缓冲一律用它；不用纳秒是因为 JS number 装不下会丢精度。
	TimestampMs int64 `json:"timestampMs"`
	// Registers 报文里的原始寄存器（按线序排列，未做字序重排）。
	// 排查字序问题时比工程值好用，界面"寄存器"模式直接展示它。
	Registers []uint16 `json:"registers,omitempty"`
	// HexValue / BinValue 是 RawValue 的机器位模式字符串：
	// UInt16 为 16 位，Float32/Int32 为 32 位，Double64 为 64 位。
	// 在后端算是因为 float32 经 JSON 转成 float64 后尾数会变形，前端反推位模式不可靠。
	HexValue string `json:"hexValue,omitempty"`
	BinValue string `json:"binValue,omitempty"`
	// AlarmState 当前报警方向："" 正常 / "High" 超上限 / "Low" 低于下限。
	// 由 Hub 在采集后回填，表格据此染色，无需前端再算一遍阈值。
	AlarmState string `json:"alarmState,omitempty"`

	ErrorMessage string `json:"errorMessage,omitempty"`
}

// FailedValue 构造一个读取失败的值
func FailedValue(tagID string, err string) TagValue {
	return TagValue{
		TagID:        tagID,
		Quality:      "Bad",
		DisplayValue: "ERR",
		Timestamp:    Now(),
		TimestampMs:  NowMs(),
		ErrorMessage: err,
	}
}

// LogEntry 日志条目
type LogEntry struct {
	Timestamp   string `json:"timestamp"`
	Level       string `json:"level"`
	ChannelName string `json:"channelName"`
	Message     string `json:"message"`
}

// Now 统一的时间戳格式
func Now() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// NowMs 当前毫秒时间戳
func NowMs() int64 {
	return time.Now().UnixMilli()
}

// FormatMs 把毫秒时间戳格式化成界面显示的时间
func FormatMs(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04:05.000")
}

// FormatClock 把毫秒时间戳格式化成 HH:MM:SS
func FormatClock(ms int64) string {
	if ms <= 0 {
		return "--"
	}
	return time.UnixMilli(ms).Format("15:04:05")
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
