package modbus

// CRC16 Modbus（多项式 0xA001，低位在前）。
// 导出来是给测试与工具用的：写用例时现场算出正确的 CRC，
// 比手抄一个十六进制常量靠谱。
func CRC16(data []byte) uint16 {
	return crc16(data)
}

// crc16 Modbus（多项式 0xA001，低位在前）
func crc16(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// appendCRC 在帧尾追加 CRC（低字节在前）
func appendCRC(frame []byte) []byte {
	c := crc16(frame)
	return append(frame, byte(c&0xFF), byte(c>>8))
}

// checkCRC 校验整帧（含 CRC）是否正确
func checkCRC(frame []byte) bool {
	if len(frame) < 2 {
		return false
	}
	want := uint16(frame[len(frame)-2]) | uint16(frame[len(frame)-1])<<8
	return crc16(frame[:len(frame)-2]) == want
}
