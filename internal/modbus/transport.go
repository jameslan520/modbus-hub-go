package modbus

import (
	"time"
)

// Transport 线路层：串口、TCP 各实现一份。
//
// 它只认识字节流——把一整帧原样送出去、再原样读回来，不做任何 Modbus 解析。
// Modbus 的内容由 Master 拼装与拆解，介质之间的差异由 Framing 抹平：
// 这样加一种链路（比如 RTU over TCP）只需要再写一个 Transport，主站逻辑一行不用动。
type Transport interface {
	// Open 建立连接：串口打开端口，TCP 拨号连到设备端口
	Open() error
	Close() error
	// IsOpen 链路是否可用。Close 之后必须为 false，主站靠它判断该不该接着重试
	IsOpen() bool
	// Write 写一整帧
	Write(frame []byte) error
	// Read 读满 n 字节。
	//
	// 超时或链路断开时要把**已经收到的部分**连同错误一起返回：
	// 排查现场故障时，"从机回了 3 个字节就断了"比一句干巴巴的"超时"有用得多。
	Read(n int, timeout time.Duration) ([]byte, error)
	// Drain 丢弃缓冲区里的残留字节，免得上一帧的尾巴把下一帧带偏
	Drain() error
	// Endpoint 连接目标的文本描述，写日志用：串口是 "COM1"，网口是 "192.168.1.10:502"
	Endpoint() string
}
