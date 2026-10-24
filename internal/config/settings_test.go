package config

import "testing"

// 历史查询空白、曲线却有数据，根因就是落库默认关着。
// 这条用例把"新装默认开启"钉死，防止以后又被改回去。
func TestDefaultHistorySettingsEnabled(t *testing.T) {
	s := DefaultHistorySettings()
	if !s.Enabled {
		t.Fatalf("落库默认应为开启，实际 %+v", s)
	}
	if s.RetentionDays != 30 || s.MaxSizeMB != 512 {
		t.Fatalf("落库默认保留/上限变了：%d 天 / %d MB", s.RetentionDays, s.MaxSizeMB)
	}
}

// 默认策略必须过 Validate，否则 LoadHistorySettings 会把它改掉
func TestDefaultHistorySettingsValidates(t *testing.T) {
	def := DefaultHistorySettings()
	if got := def.Validate(); got != def {
		t.Fatalf("默认策略被 Validate 改动了：%+v → %+v", def, got)
	}
}
