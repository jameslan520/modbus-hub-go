package models

import (
	"encoding/json"
	"testing"
)

// 旧版 channels.json 里没有 connType / host / port，反序列化后必须是串口 + 默认端口，
// 否则老用户升级上来所有通道都会变成 TCP
func TestOldChannelJSONDefaultsToRTU(t *testing.T) {
	const old = `{"id":"c1","name":"1号","portName":"COM3","baudRate":19200,"slaveId":2,"tags":[]}`
	var c ChannelConfig
	if err := json.Unmarshal([]byte(old), &c); err != nil {
		t.Fatalf("旧配置应能正常解析：%v", err)
	}
	if c.ConnType != ConnRTU {
		t.Fatalf("旧配置应落在串口 RTU，实际 %d", c.ConnType)
	}
	if c.PortName != "COM3" || c.BaudRate != 19200 {
		t.Fatalf("串口参数不对：%+v", c)
	}
	c.Sanitize()
	if c.Port != DefaultTCPPort {
		t.Fatalf("Sanitize 后 TCP 端口应补默认值 502，实际 %d", c.Port)
	}
	// 串口通道的签名里不能掺 TCP 参数
	if c.ConnSignature() != "rtu|COM3|19200|0|0|0" {
		t.Fatalf("串口签名不对：%q", c.ConnSignature())
	}
}

func TestConnSignatureIncludesTCPEndpoint(t *testing.T) {
	base := ChannelConfig{ConnType: ConnTCP, Host: "192.168.1.10", Port: 502}
	other := base
	other.Host = "192.168.1.11"
	if base.ConnSignature() == other.ConnSignature() {
		t.Fatal("改了目标 IP 签名必须变，否则改完 IP 还在旧连接上轮询")
	}
	sameHost := base
	sameHost.Port = 503
	if base.ConnSignature() == sameHost.ConnSignature() {
		t.Fatal("改了端口签名也必须变")
	}
	if base.ConnSignature() != "tcp|192.168.1.10|502" {
		t.Fatalf("TCP 签名不对：%q", base.ConnSignature())
	}
}

// 从机号随每帧报文下发，改它不应该触发重连
func TestConnSignatureIgnoresSlaveID(t *testing.T) {
	a := ChannelConfig{PortName: "COM1", BaudRate: 9600, SlaveID: 1}
	b := a
	b.SlaveID = 9
	if a.ConnSignature() != b.ConnSignature() {
		t.Fatal("从机号不该进连接签名")
	}
}

func TestEndpointText(t *testing.T) {
	rtu := ChannelConfig{PortName: "COM9", BaudRate: 115200}
	if got := rtu.EndpointText(); got != "COM9 · 115200" {
		t.Fatalf("串口连接摘要不对：%q", got)
	}
	tcp := ChannelConfig{ConnType: ConnTCP, Host: "10.0.0.7", Port: 502}
	if got := tcp.EndpointText(); got != "10.0.0.7:502" {
		t.Fatalf("网口连接摘要不对：%q", got)
	}
	// IPv6 要带方括号，不然 net.JoinHostPort 拼出来的地址是错的
	v6 := ChannelConfig{ConnType: ConnTCP, Host: "::1", Port: 502}
	if got := v6.EndpointText(); got != "[::1]:502" {
		t.Fatalf("IPv6 摘要不对：%q", got)
	}
}

func TestParseConnType(t *testing.T) {
	cases := map[string]ConnType{
		"tcp":       ConnTCP,
		"TCP":       ConnTCP,
		"modbustcp": ConnTCP,
		" rtu ":     ConnRTU,
		"":          ConnRTU,
		"乱七八糟的东西":   ConnRTU,
	}
	for in, want := range cases {
		if got := ParseConnType(in); got != want {
			t.Fatalf("ParseConnType(%q) 应为 %v，实际 %v", in, want, got)
		}
	}
	if ConnTCP.Label() != "Modbus TCP" || ConnRTU.Label() != "串口 RTU" {
		t.Fatal("连接方式的中文名不对")
	}
	if ConnType(99).IsValid() {
		t.Fatal("越界的连接方式应判为非法")
	}
}

func TestSanitizeTCPPort(t *testing.T) {
	c := ChannelConfig{ConnType: ConnTCP, Host: " 192.168.1.10 ", Port: 0}
	c.Sanitize()
	if c.Host != "192.168.1.10" {
		t.Fatalf("主机名应去空格，实际 %q", c.Host)
	}
	if c.Port != DefaultTCPPort {
		t.Fatalf("端口为空应补 502，实际 %d", c.Port)
	}
	c.Port = 70000
	c.Sanitize()
	if c.Port != DefaultTCPPort {
		t.Fatalf("越界端口应回到 502，实际 %d", c.Port)
	}
	c.Port = 1502
	c.Sanitize()
	if c.Port != 1502 {
		t.Fatalf("合法端口应保留，实际 %d", c.Port)
	}
}

// 新增通道默认走串口，且 TCP 参数已经预填一组能用值，
// 界面上切到 TCP 不用从头再敲一遍
func TestNewChannelConfigDefaults(t *testing.T) {
	c := NewChannelConfig("通道1")
	c.Sanitize()
	if c.ConnType != ConnRTU {
		t.Fatalf("新通道默认应走串口，实际 %v", c.ConnType)
	}
	if c.Host == "" || c.Port != DefaultTCPPort {
		t.Fatalf("应预填默认 TCP 参数，实际 host=%q port=%d", c.Host, c.Port)
	}
}

// 非法连接方式不能让程序扛着跑 Sanitize 之后必须回落到串口
func TestSanitizeUnknownConnType(t *testing.T) {
	c := ChannelConfig{ConnType: ConnType(7)}
	c.Sanitize()
	if c.ConnType != ConnRTU {
		t.Fatalf("未知连接方式应回落到串口，实际 %v", c.ConnType)
	}
}
