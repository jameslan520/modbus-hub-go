package modbus

import (
	"fmt"
	"sync/atomic"

	"modbus-hub-go/internal/models"
)

// Framing 报文封装规则。
//
// Modbus 的 PDU（功能码 + 数据）在 RTU 和 TCP 上逐字节相同，
// 差别只在 ADU 怎么加壳：
//
//	RTU：帧首一个从机地址，帧尾两字节 CRC，靠总线静默间隔定帧
//	TCP：帧首 7 字节 MBAP 头，没有校验尾，靠头里的长度字段定帧
//
// 把这两套壳的差异收敛在这里，Master 里就只剩纯粹的 PDU 逻辑。
type Framing interface {
	// Protocol 协议名，写日志与报文详情用："RTU" / "TCP"
	Protocol() string
	// Build 把请求 PDU 封装成要发出去的完整帧
	Build(slave byte, pdu []byte) []byte
	// Head 判定整帧长度之前至少要先读到的字节数。
	// RTU 读到第 3 个字节就能算出整帧多长（功能码 + 字节数），TCP 要读满整个 MBAP 头
	Head() int
	// Total 依据已读到的头部推算整帧字节数；头部还不全或推算不出来时返回 0
	Total(head []byte) int
	// Expected 兜底期望长度：按请求 PDU 推算响应帧应该多少字节。
	// 只有在 Total 推不出来时才用（比如从机回了语气完全不对的功能码）
	Expected(pdu []byte) int
	// Check 校验响应帧与请求帧是否配对：CRC 与从机地址（RTU）、事务标识与长度（TCP）。
	// 返回空串表示通过，否则返回写给人看的原因
	Check(req, resp []byte) string
	// Slave 从帧里取出从机地址 / 单元标识
	Slave(frame []byte) byte
	// FuncCode 取出功能码字节（含最高位的异常标志）
	FuncCode(frame []byte) byte
	// Body 取出数据域，即功能码之后、帧尾之前的部分
	Body(frame []byte) []byte}

// ---------------- Modbus RTU ----------------

// rtuAddr / rtuCRC 是在重复 needless magic number 和 readability 之间取的平衡
const (
	// rtuHead 定帧所需的前导字节数：从机地址 + 功能码 + 数据域首字节
	rtuHead = 3
	// rtuCRC CRC16 占两字节，跟在数据域后面
	rtuCRC = 2
)

// RTUFraming 串口 RTU 的封装规则
type RTUFraming struct{}

func (RTUFraming) Protocol() string { return "RTU" }

func (RTUFraming) Build(slave byte, pdu []byte) []byte {
	frame := make([]byte, 0, 1+len(pdu)+rtuCRC)
	frame = append(frame, slave)
	frame = append(frame, pdu...)
	return appendCRC(frame)
}

func (RTUFraming) Head() int { return rtuHead }

// Total 依据已经读到的前 3 个字节推算整帧长度。
// 这一改动顺带修掉了一个老毛病：以前直接按"请求应有的长度"一次性读，
// 从机返回 5 字节的异常响应时，读 9 字节的那次请求必然超时，
// 于是异常详情全被埋在"超时"里，还要白等三个重试。
func (RTUFraming) Total(head []byte) int {
	if len(head) < rtuHead {
		return 0
	}
	fc := head[1]
	if fc&0x80 != 0 {
		// 异常响应固定 5 字节：地址 + 功能码|0x80 + 异常码 + CRC
		return 5
	}
	switch fc {
	case models.FCReadHoldingRegisters:
		// 数据域第一个字节是"后面还有多少字节"，一个寄存器两个字节
		return rtuHead + int(head[2]) + rtuCRC
	case models.FCWriteSingleRegister, models.FCWriteMultipleRegisters:
		return 8
	default:
		return 0
	}
}

func (RTUFraming) Expected(pdu []byte) int {
	if len(pdu) == 0 {
		return 0
	}
	switch pdu[0] {
	case models.FCReadHoldingRegisters:
		if len(pdu) < 5 {
			return 0
		}
		return 5 + int(beUint16(pdu, 3))*2
	case models.FCWriteSingleRegister, models.FCWriteMultipleRegisters:
		return 8
	default:
		return 0
	}
}

func (RTUFraming) Check(req, resp []byte) string {
	if len(resp) < 4 {
		return fmt.Sprintf("响应帧长不足（只有 %d 字节，最小帧长 4）", len(resp))
	}
	if !checkCRC(resp) {
		return "CRC 校验失败，已丢弃并重发"
	}
	if resp[0] != req[0] {
		return fmt.Sprintf("响应从机地址不符（期望 %d，收到 %d）", req[0], resp[0])
	}
	return ""
}

func (RTUFraming) Slave(frame []byte) byte {
	if len(frame) == 0 {
		return 0
	}
	return frame[0]
}

func (RTUFraming) FuncCode(frame []byte) byte {
	if len(frame) < 2 {
		return 0
	}
	return frame[1]
}

func (RTUFraming) Body(frame []byte) []byte {
	if len(frame) < 2+rtuCRC {
		return nil
	}
	return frame[2 : len(frame)-rtuCRC]
}

// ---------------- Modbus TCP ----------------

// MBAP 头部布局（Modbus TCP 的应用数据单元头）：
//
//	0..1  事务标识 Transaction Identifier   主站生成、从站原样回显
//	2..3  协议标识 Protocol Identifier      Modbus 恒为 0x0000
//	4..5  长度     Length                   其后还有多少字节（单元标识 + PDU）
//	6     单元标识 Unit Identifier          串行链路上的从机地址，走网关时由网关解释
const (
	mbapHead = 7 // MBAP 头一共 7 字节
	mbapMax  = 260
)

// TCPFraming Modbus TCP 的封装规则。
// 事务标识每次组帧自增、从站原样回显，据此判断收到的帧是不是本次请求的应答。
type TCPFraming struct {
	txid atomic.Uint32
}

func (*TCPFraming) Protocol() string { return "TCP" }

func (f *TCPFraming) Build(slave byte, pdu []byte) []byte {
	id := uint16(f.txid.Add(1) & 0xFFFF)
	length := len(pdu) + 1 // 单元标识 + PDU
	frame := make([]byte, 0, mbapHead+len(pdu))
	frame = append(frame, byte(id>>8), byte(id), 0x00, 0x00, byte(length>>8), byte(length), slave)
	return append(frame, pdu...)
}

func (*TCPFraming) Head() int { return mbapHead }

func (*TCPFraming) Total(head []byte) int {
	if len(head) < mbapHead {
		return 0
	}
	// 长度字段统计的是它自己之后的字节数，加上头部 6 字节就是整帧长度
	n := 6 + int(beUint16(head, 4))
	// 最短也得是 MBAP + 功能码 + 异常码；最长不超过 ADU 上限 260。
	// 越界的长度字段说明这帧根本不是 Modbus，别照着它去读未知长度的数据
	if n < mbapHead+2 || n > mbapMax {
		return 0
	}
	return n
}

// Expected TCP 上不需要兜底期望长度：长度字段写在 MBAP 头里，Total 永远推得出来。
// 真推不出来（长度字段离谱）说明这条流已经失同步，硬读只会越读越乱。
func (*TCPFraming) Expected(pdu []byte) int { return 0 }

func (*TCPFraming) Check(req, resp []byte) string {
	if len(resp) < mbapHead {
		return fmt.Sprintf("响应帧长不足（只有 %d 字节，MBAP 头就要 7）", len(resp))
	}
	if pid := beUint16(resp, 2); pid != 0 {
		return fmt.Sprintf("协议标识不为 0（收到 0x%04X），不是 Modbus TCP 报文", pid)
	}
	if len(req) >= mbapHead {
		if want, got := beUint16(req, 0), beUint16(resp, 0); want != got {
			return fmt.Sprintf("事务标识不符（期望 0x%04X，收到 0x%04X），不是本次请求的应答", want, got)
		}
	}
	if want := 6 + int(beUint16(resp, 4)); want != len(resp) {
		return fmt.Sprintf("MBAP 长度字段与实际帧长不符（标明 %d，实际 %d）", want, len(resp))
	}
	return ""
}

func (*TCPFraming) Slave(frame []byte) byte {
	if len(frame) < mbapHead {
		return 0
	}
	return frame[6]
}

func (*TCPFraming) FuncCode(frame []byte) byte {
	if len(frame) < mbapHead+1 {
		return 0
	}
	return frame[mbapHead]
}

// Body 取出**功能码之后**、帧尾之前的数据域。
//
// 两种介质在这里刻意定义成一样的：RTU 剥掉帧首地址与帧尾 CRC 后还要再
// 跳过功能码，TCP 剥掉 MBAP 头后同样要跳过功能码。
// 统一口径 Master 才好按同一个下标解数，否则光是对齐偏移就够埋bug 了。
func (*TCPFraming) Body(frame []byte) []byte {
	if len(frame) < mbapHead+1 {
		return nil
	}
	return frame[mbapHead+1:]
}
