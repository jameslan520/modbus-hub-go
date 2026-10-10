package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"modbus-hub-go/internal/store"
)

// SettingsPath 与 exe 同目录下的 settings.json。
// 单独成文件而不是塞进 channels.json，是为了保持 channels.json 与 C# 版结构一致。
func SettingsPath() string {
	return filepath.Join(Dir(), "settings.json")
}

// HistoryDir 历史库目录：exe 同目录下的 history/
func HistoryDir() string {
	return filepath.Join(Dir(), "history")
}

// DefaultHistorySettings 首次运行的默认策略：默认关闭，保留 30 天、上限 512MB
func DefaultHistorySettings() store.Settings {
	return store.Settings{Enabled: false, RetentionDays: 30, MaxSizeMB: 512}
}

// LoadHistorySettings 读取落库策略；读不到就用默认值
func LoadHistorySettings() store.Settings {
	def := DefaultHistorySettings()
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		return def
	}
	var s store.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return def
	}
	return s.Validate()
}

// SaveHistorySettings 写入落库策略
func SaveHistorySettings(s store.Settings) error {
	s = s.Validate()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(SettingsPath(), data, 0o644)
}
