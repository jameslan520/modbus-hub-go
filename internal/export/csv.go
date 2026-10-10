package export

import (
	"os"
	"strings"
)

// BOM Excel 打开 UTF-8 CSV 时靠它识别编码，否则中文会乱码
var BOM = []byte{0xEF, 0xBB, 0xBF}

// WriteCSV 以 UTF-8 BOM + CRLF 写出 CSV 文件
func WriteCSV(path string, header []string, rows [][]string) error {
	var sb strings.Builder
	sb.Grow(1024)

	writeLine(&sb, header)
	for _, row := range rows {
		writeLine(&sb, row)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(BOM); err != nil {
		return err
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		return err
	}
	return f.Sync()
}

func writeLine(sb *strings.Builder, fields []string) {
	for i, field := range fields {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(Escape(field))
	}
	sb.WriteString("\r\n")
}

// Escape 含逗号、引号、换行、回车的字段加双引号，内部引号翻倍（RFC 4180）
func Escape(s string) string {
	if !strings.ContainsAny(s, ",\"\r\n") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
