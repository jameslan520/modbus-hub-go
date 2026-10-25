package models

import (
	"net"
	"strings"
)

// Modbus RTU 功能码：03 读保持寄存器、06 写单个寄存器、16 写多个寄存器
const (
	FCReadHoldingRegisters   byte = 0x03
	FCWriteSingleRegister    byte = 0x06
	FCWriteMultipleRegisters byte = 0x10
)

// TagDataType 与 C# TagDataType 枚举顺序保持一致（JSON 中为数字）
type TagDataType int

const (
	UInt16 TagDataType = iota
	Float32
	Double64
	Int32
)

var dataTypeNames = map[TagDataType]string{
	UInt16:   "UInt16",
	Float32:  "Float32",
	Double64: "Double64",
	Int32:    "Int32",
}

func (t TagDataType) String() string {
	if n, ok := dataTypeNames[t]; ok {
		return n
	}
	return "Float32"
}

func (t TagDataType) IsValid() bool {
	_, ok := dataTypeNames[t]
	return ok
}

// 校验位：与 System.IO.Ports.Parity 一致（0 无 / 1 奇 / 2 偶 / 3 Mark / 4 Space）
const (
	ParityNone  int = 0
	ParityOdd   int = 1
	ParityEven  int = 2
	ParityMark  int = 3
	ParitySpace int = 4
)

// 停止位：与 System.IO.Ports.StopBits 一致（1 = 1 位，2 = 2 位）
const (
	StopBitsOne int = 1
	StopBitsTwo int = 2
)

// CalibrationWriteMode 标定写入模式
type CalibrationWriteMode int

const (
	CalibrationTable          CalibrationWriteMode = 0
	CalibrationSlopeIntercept CalibrationWriteMode = 1
)

// ByteOrder 多寄存器数值的字节顺序。
//
// 命名沿用现场习惯：把一个数值的最高字节记作 A，依次往低记作 B、C、D（32 位）/ … H（64 位）。
// Modbus 报文本身是大端（寄存器内高字节在前），所以"字序"说的是**寄存器之间以及寄存器
// 内部字节**怎么排。四种规则对 32 位与 64 位用同一套字节置换定义，见 modbus/applyByteOrder。
//
// 零值必须是 CDAB：旧版 channels.json 没有这个字段，反序列化后就是 0，
// 行为要与旧版（Float32/Int32 = CDAB、Double64 = GHEF CDAB）完全一致。
type ByteOrder int

const (
	// ByteOrderCDAB 低字在前（默认值，与旧版一致）。32 位：[CD][AB]；64 位：GH EF CD AB
	ByteOrderCDAB ByteOrder = iota
	// ByteOrderABCD 高字在前（大端直排）。32 位：[AB][CD]；64 位：AB CD EF GH
	ByteOrderABCD
	// ByteOrderBADC 字内字节交换、字序不变。32 位：[BA][DC]；64 位：BA DC FE HG
	ByteOrderBADC
	// ByteOrderDCBA 全反序（小端）。32 位：[DC][BA]；64 位：HG FE DC BA
	ByteOrderDCBA
)

var byteOrderNames = map[ByteOrder]string{
	ByteOrderCDAB: "CDAB",
	ByteOrderABCD: "ABCD",
	ByteOrderBADC: "BADC",
	ByteOrderDCBA: "DCBA",
}

// byteOrderLabels 是界面下拉与提示用的中文说明
var byteOrderLabels = map[ByteOrder]string{
	ByteOrderCDAB: "CDAB 低字在前",
	ByteOrderABCD: "ABCD 高字在前",
	ByteOrderBADC: "BADC 字内字节交换",
	ByteOrderDCBA: "DCBA 全反序",
}

func (o ByteOrder) String() string {
	if n, ok := byteOrderNames[o]; ok {
		return n
	}
	return byteOrderNames[ByteOrderCDAB]
}

// Label 带中文说明的名称，界面下拉直接显示
func (o ByteOrder) Label() string {
	if n, ok := byteOrderLabels[o]; ok {
		return n
	}
	return byteOrderLabels[ByteOrderCDAB]
}

func (o ByteOrder) IsValid() bool {
	_, ok := byteOrderNames[o]
	return ok
}

// Applies 判断该数据类型是否受字节顺序影响。
// UInt16 只占一个寄存器，理论上也有字节交换（BADC/DCBA 对它等价），
// 但现场绝大多数设备就是 ABCD，列出来徒增困惑，界面上不显示。
func (o ByteOrder) Applies(dt TagDataType) bool {
	return dt == Float32 || dt == Int32 || dt == Double64
}

// InputMode 写入窗口的输入口径。
// 三种口径说的是同一个寄存器里的内容，只是站的角度不同：
//
//	raw 原始值 —— 寄存器里真正放的十进制数值，不乘倍率不加偏移
//	hex 十六进制 —— 按 IEEE754 位模式填，跟点表里"十六进制"列显示的是一套东西
//	eng 工程值 —— 带倍率/偏移的现场读数，由程序反算成原始值再下发
type InputMode string

const (
	InputModeRaw InputMode = "raw"
	InputModeHex InputMode = "hex"
	InputModeEng InputMode = "eng"
)

var inputModeNames = map[InputMode]string{
	InputModeRaw: "raw",
	InputModeHex: "hex",
	InputModeEng: "eng",
}

var inputModeLabels = map[InputMode]string{
	InputModeRaw: "原始值（十进制）",
	InputModeHex: "十六进制（位模式）",
	InputModeEng: "工程值（反算）",
}

// ParseInputMode 把界面传来的字符串转成输入口径，空值与非法值一律当原始值处理
func ParseInputMode(s string) InputMode {
	m := InputMode(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := inputModeNames[m]; ok {
		return m
	}
	return InputModeRaw
}

func (m InputMode) String() string {
	if n, ok := inputModeNames[m]; ok {
		return n
	}
	return string(InputModeRaw)
}

// Label 界面下拉显示的中文名
func (m InputMode) Label() string {
	if n, ok := inputModeLabels[m]; ok {
		return n
	}
	return inputModeLabels[InputModeRaw]
}

// ChannelState 通道运行状态
type ChannelState string

const (
	StateDisconnected ChannelState = "Disconnected"
	StateConnecting   ChannelState = "Connecting"
	StateConnected    ChannelState = "Connected"
	StateError        ChannelState = "Error"
)

// TagPoint SCADA 数据点。采集一律用 03；06/16 表示可写。
type TagPoint struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	SlaveID      byte        `json:"slaveId"`
	FunctionCode byte        `json:"functionCode"`
	Address      uint16      `json:"address"`
	DataType     TagDataType `json:"dataType"`
	// 多寄存器数值的字节顺序；零值 CDAB 与旧版一致，改它不会触发串口重连
	ByteOrder     ByteOrder `json:"byteOrder"`
	Unit          string    `json:"unit"`
	Scale         float64   `json:"scale"`
	Offset        float64   `json:"offset"`
	DecimalPlaces int       `json:"decimalPlaces"`
	TrendEnabled  bool      `json:"trendEnabled"`
	IsEnabled     bool      `json:"isEnabled"`

	// 报警：上下限用指针区分"没配"（null）和"配成 0"，0 是合法阈值。
	// Deadband 是回差，回落到 (限值 ∓ 回差) 以内才算恢复，避免临界值反复报警。
	// DelaySec 是持续超限多久才报警，0 表示立即。
	// ActionTagID/ActionValue 是联动输出：报警触发（上升沿）时把值写进另一个可写点。
	AlarmEnabled    bool     `json:"alarmEnabled"`
	AlarmHigh       *float64 `json:"alarmHigh,omitempty"`
	AlarmLow        *float64 `json:"alarmLow,omitempty"`
	AlarmDeadband   float64  `json:"alarmDeadband"`
	AlarmDelaySec   int      `json:"alarmDelaySec"`
	AlarmActionTag  string   `json:"alarmActionTag,omitempty"`
	AlarmActionText string   `json:"alarmActionText,omitempty"`
}

// ConnType 通道的连接方式。
//
// 零值必须是串口 RTU：旧版 channels.json 没有这个字段，反序列化后就是 0，
// 行为要与旧版完全一致（一律走串口）。
type ConnType int

const (
	// ConnRTU 串口 RTU（默认值，与旧版一致）
	ConnRTU ConnType = iota
	// ConnTCP Modbus TCP/IP（网口直连或从串口服务器转出来的网口）
	ConnTCP
)

// DefaultTCPPort Modbus TCP 的公认端口
const DefaultTCPPort = 502

var connTypeNames = map[ConnType]string{
	ConnRTU: "rtu",
	ConnTCP: "tcp",
}

var connTypeLabels = map[ConnType]string{
	ConnRTU: "串口 RTU",
	ConnTCP: "Modbus TCP",
}

func (t ConnType) String() string {
	if n, ok := connTypeNames[t]; ok {
		return n
	}
	return connTypeNames[ConnRTU]
}

// Label 界面下拉显示的中文名
func (t ConnType) Label() string {
	if n, ok := connTypeLabels[t]; ok {
		return n
	}
	return connTypeLabels[ConnRTU]
}

func (t ConnType) IsValid() bool {
	_, ok := connTypeNames[t]
	return ok
}

// ParseConnType 把界面传来的字符串转成连接方式，空值与非法值一律当串口
func ParseConnType(s string) ConnType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tcp", "modbustcp", "modbus-tcp":
		return ConnTCP
	case "rtu", "serial":
		return ConnRTU
	default:
		return ConnRTU
	}
}

// IsTCP 是否走网口
func (t ConnType) IsTCP() bool { return t == ConnTCP }

// AlarmKind 报警方向。空串表示未报警。
type AlarmKind string

const (
	AlarmNone AlarmKind = ""
	AlarmHigh AlarmKind = "High"
	AlarmLow  AlarmKind = "Low"
)

// HighLimit / LowLimit 安全取阈值，没配时返回零值且 ok=false
func (t TagPoint) LimitHigh() (float64, bool) {
	if t.AlarmHigh == nil {
		return 0, false
	}
	return *t.AlarmHigh, true
}

func (t TagPoint) LimitLow() (float64, bool) {
	if t.AlarmLow == nil {
		return 0, false
	}
	return *t.AlarmLow, true
}

// AlarmConfigured 至少配了一个限值才算配好报警
func (t TagPoint) AlarmConfigured() bool {
	return t.AlarmHigh != nil || t.AlarmLow != nil
}

// IsWritable 功能码 06/16 的点可写
func (t TagPoint) IsWritable() bool {
	return t.FunctionCode == FCWriteSingleRegister || t.FunctionCode == FCWriteMultipleRegisters
}

// RegisterCount 该数据类型占用的寄存器数
func (t TagPoint) RegisterCount() int {
	switch t.DataType {
	case Float32, Int32:
		return 2
	case Double64:
		return 4
	default:
		return 1
	}
}

func (t *TagPoint) Sanitize(slaveID byte) {
	t.Name = strings.TrimSpace(t.Name)
	t.Unit = strings.TrimSpace(t.Unit)
	t.SlaveID = slaveID
	if t.DecimalPlaces < 0 || t.DecimalPlaces > 12 {
		t.DecimalPlaces = 2
	}
	if t.FunctionCode != FCReadHoldingRegisters &&
		t.FunctionCode != FCWriteSingleRegister &&
		t.FunctionCode != FCWriteMultipleRegisters {
		t.FunctionCode = FCReadHoldingRegisters
	}
	if !t.DataType.IsValid() {
		t.DataType = Float32
	}
	if !t.ByteOrder.IsValid() {
		t.ByteOrder = ByteOrderCDAB
	}
	if !t.AlarmConfigured() {
		t.AlarmEnabled = false
	}
	if t.AlarmDeadband < 0 {
		t.AlarmDeadband = 0
	}
	if t.AlarmDelaySec < 0 {
		t.AlarmDelaySec = 0
	}
	if t.AlarmDelaySec > 3600 {
		t.AlarmDelaySec = 3600
	}
	t.AlarmActionTag = strings.TrimSpace(t.AlarmActionTag)
	t.AlarmActionText = strings.TrimSpace(t.AlarmActionText)
	if t.AlarmActionTag == "" {
		t.AlarmActionText = ""
	}
}

// CalibrationPoint 标定点（测量值 / 标准值）
type CalibrationPoint struct {
	Measured float64 `json:"measured"`
	Standard float64 `json:"standard"`
}

// ChannelConfig 通道配置。
//
// 一个通道对应一条物理链路：串口就是一个 COM 口，网口就是一个 IP + 端口。
// 两种链路上的 Modbus 点对点问答完全相同，差别只在介质参数，
// 所以串口参数与 TCP 参数都放在这一个结构里，靠 ConnType 区分用哪一半。
type ChannelConfig struct {
	ID string `json:"id"`
	// ConnType 连接方式；旧配置没有这个字段，反序列化为 0（串口），与旧版一致
	ConnType ConnType `json:"connType"`
	Name     string   `json:"name"`

	// 串口参数（ConnType = rtu 时有效）
	PortName string `json:"portName"`
	BaudRate int    `json:"baudRate"`
	DataBits int    `json:"dataBits"`
	Parity   int    `json:"parity"`
	StopBits int    `json:"stopBits"`

	// TCP 参数（ConnType = tcp 时有效）。
	// Host 可以是 IP（192.168.1.10）也可以是主机名；Port 留空时补 502。
	// 整个通道共用一个从机号（写进 MBAP 的单元标识），多从机走网关的场景用 255 或网关自报的号。
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`

	ReadTimeout       int        `json:"readTimeout"`
	WriteTimeout      int        `json:"writeTimeout"`
	PollingIntervalMs int        `json:"pollingIntervalMs"`
	SlaveID           byte       `json:"slaveId"`
	IsEnabled         bool       `json:"isEnabled"`
	Tags              []TagPoint `json:"tags"`

	CalibrationTagID        string      `json:"calibrationTagId,omitempty"`
	CalibrationSampleCount  int         `json:"calibrationSampleCount"`
	CalibrationStartAddress uint16      `json:"calibrationStartAddress"`
	CalibrationDataType     TagDataType `json:"calibrationDataType"`
	// CalibrationByteOrder 标定下载的字节顺序，零值 CDAB 与旧版一致
	CalibrationByteOrder ByteOrder            `json:"calibrationByteOrder"`
	CalibrationWriteMode CalibrationWriteMode `json:"calibrationWriteMode"`
	CalibrationPoints    []CalibrationPoint   `json:"calibrationPoints"`
}

// ConnSignature 连接参数签名，变了就必须重连。
//
// 之前只签串口参数（老名字 SerialSignature），加了 TCP 之后必须把 IP/端口
// 也算进去：改了目标 IP 却还在旧连接上轮询，是最难发现的一类故障。
// 从机号不算在里头——它随每帧报文下发，改不用重连。
func (c ChannelConfig) ConnSignature() string {
	if c.ConnType.IsTCP() {
		return "tcp|" + c.Host + "|" + itoa(c.Port)
	}
	return "rtu|" + c.PortName + "|" + itoa(c.BaudRate) + "|" + itoa(c.DataBits) +
		"|" + itoa(c.Parity) + "|" + itoa(c.StopBits)
}

// EndpointText 连接目标的显示文本，日志与界面摘要都用这一份：
// 串口是 "COM1 · 9600"，网口是 "192.168.1.10:502"。
func (c ChannelConfig) EndpointText() string {
	if c.ConnType.IsTCP() {
		return net.JoinHostPort(strings.TrimSpace(c.Host), itoa(c.Port))
	}
	return c.PortName + " · " + itoa(c.BaudRate)
}

func (c *ChannelConfig) Sanitize() {
	c.Name = strings.TrimSpace(c.Name)
	c.PortName = strings.TrimSpace(c.PortName)
	c.Host = strings.TrimSpace(c.Host)
	if !c.ConnType.IsValid() {
		c.ConnType = ConnRTU
	}
	// 端口没填或填了非法值一律补默认端口。
	// 串口通道也顺手补上：界面上把连接方式切到 TCP 时不用重新敲一遍，
	// 也省得通道启动到一半才发现拨不出去。
	if c.Port < 1 || c.Port > 65535 {
		c.Port = DefaultTCPPort
	}
	if c.SlaveID < 1 || c.SlaveID > 247 {
		from := byte(1)
		if len(c.Tags) > 0 && c.Tags[0].SlaveID >= 1 && c.Tags[0].SlaveID <= 247 {
			from = c.Tags[0].SlaveID
		}
		c.SlaveID = from
	}
	for i := range c.Tags {
		c.Tags[i].Sanitize(c.SlaveID)
	}
	switch c.CalibrationSampleCount {
	case 5, 8, 10, 15, 20:
	default:
		c.CalibrationSampleCount = 8
	}
	if !c.CalibrationDataType.IsValid() {
		c.CalibrationDataType = Float32
	}
	if !c.CalibrationByteOrder.IsValid() {
		c.CalibrationByteOrder = ByteOrderCDAB
	}
	if c.CalibrationWriteMode != CalibrationTable && c.CalibrationWriteMode != CalibrationSlopeIntercept {
		c.CalibrationWriteMode = CalibrationTable
	}
	if c.CalibrationPoints == nil {
		c.CalibrationPoints = []CalibrationPoint{}
	}
	valid := c.CalibrationPoints[:0]
	for _, p := range c.CalibrationPoints {
		if isFinite(p.Measured) && isFinite(p.Standard) {
			valid = append(valid, p)
		}
	}
	c.CalibrationPoints = valid
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func isFinite(v float64) bool {
	return !(v != v) && !(v > 1e308) && !(v < -1e308)
}

// NewChannelConfig 生成一个带默认值的通道
func NewChannelConfig(name string) ChannelConfig {
	return ChannelConfig{
		ID:       newID(),
		Name:     name,
		PortName: "COM1",
		BaudRate: 9600,
		DataBits: 8,
		Parity:   ParityNone,
		StopBits: StopBitsOne,
		// TCP 参数给一组能用的默认值：界面上把连接方式切到 TCP 时不用重新敲一遍
		Host:                   "192.168.1.10",
		Port:                   DefaultTCPPort,
		ReadTimeout:            1000,
		WriteTimeout:           1000,
		PollingIntervalMs:      1000,
		SlaveID:                1,
		IsEnabled:              true,
		Tags:                   []TagPoint{},
		CalibrationSampleCount: 8,
		CalibrationDataType:    Float32,
		CalibrationPoints:      []CalibrationPoint{},
	}
}

// NewTagPoint 生成一个带默认值的点
func NewTagPoint(name string) TagPoint {
	return TagPoint{
		ID:            newID(),
		Name:          name,
		SlaveID:       1,
		FunctionCode:  FCReadHoldingRegisters,
		DataType:      Float32,
		ByteOrder:     ByteOrderCDAB,
		Scale:         1,
		DecimalPlaces: 2,
		TrendEnabled:  true,
		IsEnabled:     true,
	}
}
