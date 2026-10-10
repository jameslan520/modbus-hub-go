package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"modbus-hub-go/internal/models"
)

// Dir 配置文件所在目录（exe 同目录），导出文件的默认落点
func Dir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// ConfigPath 与 exe 同目录下的 channels.json
func ConfigPath() string {
	return filepath.Join(Dir(), "channels.json")
}

// Load 读取配置；不存在时生成默认配置
func Load() []models.ChannelConfig {
	path := ConfigPath()
	if _, err := os.Stat(path); err == nil {
		data, err := os.ReadFile(path)
		if err == nil {
			var list []models.ChannelConfig
			if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
				for i := range list {
					list[i].Sanitize()
				}
				return list
			}
		}
	}
	def := DefaultChannels()
	_ = Save(def)
	return def
}

// Save 写入配置
func Save(list []models.ChannelConfig) error {
	for i := range list {
		list[i].Sanitize()
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// DefaultChannels 首次运行时的默认通道
func DefaultChannels() []models.ChannelConfig {
	ch := models.NewChannelConfig("通道1")
	ch.PortName = "COM1"
	ch.BaudRate = 9600
	ch.PollingIntervalMs = 1000
	ch.SlaveID = 1

	temp := models.NewTagPoint("温度")
	temp.SlaveID = 1
	temp.FunctionCode = models.FCReadHoldingRegisters
	temp.Address = 0
	temp.DataType = models.Float32
	temp.Unit = "°C"

	setpoint := models.NewTagPoint("设定值")
	setpoint.SlaveID = 1
	setpoint.FunctionCode = models.FCWriteMultipleRegisters
	setpoint.Address = 2
	setpoint.DataType = models.Float32
	setpoint.Unit = "°C"

	ch.Tags = []models.TagPoint{temp, setpoint}
	return []models.ChannelConfig{ch}
}
