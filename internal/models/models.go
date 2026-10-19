package models

import "strings"

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

// ChannelConfig 串口通道配置（一个通道对应一个 COM 口）
type ChannelConfig struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	PortName          string     `json:"portName"`
	BaudRate          int        `json:"baudRate"`
	DataBits          int        `json:"dataBits"`
	Parity            int        `json:"parity"`
	StopBits          int        `json:"stopBits"`
	ReadTimeout       int        `json:"readTimeout"`
	WriteTimeout      int        `json:"writeTimeout"`
	PollingIntervalMs int        `json:"pollingIntervalMs"`
	SlaveID           byte       `json:"slaveId"`
	IsEnabled         bool       `json:"isEnabled"`
	Tags              []TagPoint `json:"tags"`

	CalibrationTagID        string               `json:"calibrationTagId,omitempty"`
	CalibrationSampleCount  int                  `json:"calibrationSampleCount"`
	CalibrationStartAddress uint16               `json:"calibrationStartAddress"`
	CalibrationDataType     TagDataType          `json:"calibrationDataType"`
	// CalibrationByteOrder 标定下载的字节顺序，零值 CDAB 与旧版一致
	CalibrationByteOrder    ByteOrder            `json:"calibrationByteOrder"`
	CalibrationWriteMode    CalibrationWriteMode `json:"calibrationWriteMode"`
	CalibrationPoints       []CalibrationPoint   `json:"calibrationPoints"`
}

// SerialSignature 串口参数签名，用于判断是否需要重连
func (c ChannelConfig) SerialSignature() string {
	return c.PortName + "|" + itoa(c.BaudRate) + "|" + itoa(c.DataBits) +
		"|" + itoa(c.Parity) + "|" + itoa(c.StopBits)
}

func (c *ChannelConfig) Sanitize() {
	c.Name = strings.TrimSpace(c.Name)
	c.PortName = strings.TrimSpace(c.PortName)
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
		ID:                     newID(),
		Name:                   name,
		PortName:               "COM1",
		BaudRate:               9600,
		DataBits:               8,
		Parity:                 ParityNone,
		StopBits:               StopBitsOne,
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
