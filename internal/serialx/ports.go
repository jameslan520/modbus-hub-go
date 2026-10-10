package serialx

import (
	"sort"
	"strconv"
	"strings"

	"go.bug.st/serial"
)

// PortInfo 串口枚举结果
type PortInfo struct {
	Name string `json:"name"`
}

// CommonBaudRates 常用波特率
var CommonBaudRates = []int{9600, 19200, 38400, 57600, 115200}

// List 列出本机可用串口（按 COM 编号排序）
func List() []PortInfo {
	names, err := serial.GetPortsList()
	if err != nil || len(names) == 0 {
		return []PortInfo{}
	}
	sort.SliceStable(names, func(i, j int) bool {
		return comNumber(names[i]) < comNumber(names[j])
	})
	out := make([]PortInfo, 0, len(names))
	for _, n := range names {
		out = append(out, PortInfo{Name: n})
	}
	return out
}

func comNumber(name string) int {
	upper := strings.ToUpper(name)
	idx := strings.Index(upper, "COM")
	if idx < 0 {
		return 1 << 20
	}
	v, err := strconv.Atoi(upper[idx+3:])
	if err != nil {
		return 1 << 20
	}
	return v
}
