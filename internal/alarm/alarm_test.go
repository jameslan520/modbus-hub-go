package alarm

import (
	"testing"
	"time"

	"modbus-hub-go/internal/models"
)

func tag(name string, high, low *float64) models.TagPoint {
	t := models.NewTagPoint(name)
	t.AlarmEnabled = true
	t.AlarmHigh = high
	t.AlarmLow = low
	t.IsEnabled = true
	t.Unit = "°C"
	return t
}

func f(v float64) *float64 { return &v }

func TestHighAlarmTriggers(t *testing.T) {
	var got []Event
	e := New(func(ev Event) { got = append(got, ev) }, nil)
	tp := tag("温度", f(80), nil)

	if k := e.Evaluate("ch", "通道1", tp, 79.9, time.Now()); k != models.AlarmNone {
		t.Fatalf("未超限不应报警: %v", k)
	}
	now := time.Now()
	k := e.Evaluate("ch", "通道1", tp, 85, now)
	if k != models.AlarmHigh {
		t.Fatalf("超上限应报警: %v", k)
	}
	if len(got) != 1 || !got[0].Active || got[0].Kind != "High" {
		t.Fatalf("应发出一条报警事件: %+v", got)
	}
	if got[0].Limit != 80 || got[0].Value != 85 {
		t.Fatalf("事件里的限值/当前值不对: %+v", got[0])
	}
	// 已报警期间再超限不应重复触发
	e.Evaluate("ch", "通道1", tp, 90, now.Add(time.Second))
	if len(got) != 1 {
		t.Fatalf("重复触发了: %d 条", len(got))
	}
	if len(e.Active()) != 1 {
		t.Fatalf("活跃报警应为 1: %d", len(e.Active()))
	}
}

func TestLowAlarm(t *testing.T) {
	var got []Event
	e := New(func(ev Event) { got = append(got, ev) }, nil)
	tp := tag("液位", nil, f(10))

	e.Evaluate("ch", "ch", tp, 9.5, time.Now())
	if len(got) != 1 || got[0].Kind != "Low" {
		t.Fatalf("低于下限应报警: %+v", got)
	}
}

func TestRecover(t *testing.T) {
	var got []Event
	e := New(func(ev Event) { got = append(got, ev) }, nil)
	tp := tag("温度", f(80), nil)

	tp.AlarmDeadband = 2
	e.Evaluate("ch", "ch", tp, 85, time.Now())
	// 回到限值以内但没到回差（80-2=78），仍算报警中
	if k := e.Evaluate("ch", "ch", tp, 79.99, time.Now()); k != models.AlarmHigh {
		t.Fatalf("回差内不应恢复: %v", k)
	}
	if k := e.Evaluate("ch", "ch", tp, 77.5, time.Now()); k != models.AlarmNone {
		t.Fatalf("落到 (上限-回差) 以内应恢复: %v", k)
	}
	if len(got) != 2 || got[1].Active {
		t.Fatalf("应发出恢复事件: %+v", got)
	}
	if len(e.Active()) != 0 {
		t.Fatalf("恢复后不应有活跃报警")
	}
}

func TestDelay(t *testing.T) {
	var got []Event
	e := New(func(ev Event) { got = append(got, ev) }, nil)
	tp := tag("温度", f(80), nil)
	tp.AlarmDelaySec = 5

	base := time.Now()
	e.Evaluate("ch", "ch", tp, 85, base)
	if len(got) != 0 {
		t.Fatalf("延时未到不应报警: %+v", got)
	}
	e.Evaluate("ch", "ch", tp, 85, base.Add(3*time.Second))
	if len(got) != 0 {
		t.Fatalf("3 秒还没到 5 秒: %+v", got)
	}
	e.Evaluate("ch", "ch", tp, 85, base.Add(6*time.Second))
	if len(got) != 1 {
		t.Fatalf("超过延时后应报警: %+v", got)
	}
	// 中途回到正常要重置计时
	e2 := New(nil, nil)
	tp2 := tag("温度", f(80), nil)
	tp2.AlarmDelaySec = 5
	e2.Evaluate("ch", "ch", tp2, 85, base)
	e2.Evaluate("ch", "ch", tp2, 70, base.Add(time.Second))
	e2.Evaluate("ch", "ch", tp2, 85, base.Add(3*time.Second))
	if len(e2.Active()) != 0 {
		t.Fatalf("中断后计时未重置，误报警")
	}
}

func TestSwitchDirection(t *testing.T) {
	var got []Event
	e := New(func(ev Event) { got = append(got, ev) }, nil)
	tp := tag("温度", f(80), f(10))

	now := time.Now()
	e.Evaluate("ch", "ch", tp, 90, now)
	e.Evaluate("ch", "ch", tp, 5, now.Add(time.Second))
	if len(got) != 3 {
		t.Fatalf("方向切换应产生 恢复+新报警 两条: %+v", got)
	}
	if got[2].Kind != "Low" || !got[2].Active {
		t.Fatalf("最后一条应为下限报警: %+v", got[2])
	}
}

func TestBadValueKeepsState(t *testing.T) {
	e := New(nil, nil)
	tp := tag("温度", f(80), nil)
	e.Evaluate("ch", "ch", tp, 90, time.Now())
	// NaN 表示采集失败，应保持原报警状态而不是误消
	if k := e.Evaluate("ch", "ch", tp, nan(), time.Now()); k != models.AlarmHigh {
		t.Fatalf("采集失败应保持报警: %v", k)
	}
}

func TestDisabledTagReleases(t *testing.T) {
	var got []Event
	e := New(func(ev Event) { got = append(got, ev) }, nil)
	tp := tag("温度", f(80), nil)
	e.Evaluate("ch", "ch", tp, 90, time.Now())

	tp.AlarmEnabled = false
	e.Evaluate("ch", "ch", tp, 90, time.Now())
	if len(e.Active()) != 0 {
		t.Fatalf("关闭报警后应释放")
	}
	if len(got) != 2 || got[1].Active {
		t.Fatalf("应发出恢复事件: %+v", got)
	}
}

func TestActionFiresOnce(t *testing.T) {
	calls := 0
	e := New(nil, func(channelID, tagID, value string) string {
		calls++
		if channelID != "ch" || tagID != "relay" || value != "1" {
			t.Fatalf("联动参数不对: %s %s %s", channelID, tagID, value)
		}
		return "ok"
	})
	tp := tag("温度", f(80), nil)
	tp.AlarmActionTag = "relay"
	tp.AlarmActionText = "1"

	e.Evaluate("ch", "ch", tp, 90, time.Now())
	e.Evaluate("ch", "ch", tp, 91, time.Now().Add(time.Second))
	if calls != 1 {
		t.Fatalf("联动只在上升沿触发一次，实际 %d 次", calls)
	}
	hist := e.History(0)
	if len(hist) != 1 || hist[0].Action != "ok" {
		t.Fatalf("联动结果应写进事件: %+v", hist)
	}
}

func TestAckAndHistory(t *testing.T) {
	e := New(nil, nil)
	tp := tag("温度", f(80), nil)
	e.Evaluate("ch", "ch", tp, 90, time.Now())
	act := e.Active()
	if len(act) != 1 {
		t.Fatalf("应有 1 条活跃报警")
	}
	if !e.Ack(act[0].ID) {
		t.Fatalf("确认失败")
	}
	if !e.Active()[0].Acked {
		t.Fatalf("确认状态未生效")
	}
	if n := e.AckAll(); n != 0 {
		t.Fatalf("已确认的不应重复计数: %d", n)
	}
	e.ClearHistory()
	if len(e.History(0)) != 1 {
		t.Fatalf("清空历史应保留活跃报警")
	}
}

func TestKeepAndReset(t *testing.T) {
	e := New(nil, nil)
	tp := tag("温度", f(80), nil)
	e.Evaluate("ch", "ch", tp, 90, time.Now())
	e.Keep(map[string]bool{})
	if len(e.Active()) != 0 || len(e.states) != 0 {
		t.Fatalf("Keep 应清掉不在保留列表里的状态")
	}
}

func TestHistoryLimit(t *testing.T) {
	e := New(nil, nil)
	tp := tag("温度", f(80), nil)
	base := time.Now()
	for i := 0; i < maxHistory+50; i++ {
		if i%2 == 0 {
			e.Evaluate("ch", "ch", tp, 90, base.Add(time.Duration(i)*time.Second))
		} else {
			e.Evaluate("ch", "ch", tp, 10, base.Add(time.Duration(i)*time.Second))
		}
	}
	if len(e.history) > maxHistory {
		t.Fatalf("历史应截断到 %d，实际 %d", maxHistory, len(e.history))
	}
}

func nan() float64 {
	z := 0.0
	return z / z
}
