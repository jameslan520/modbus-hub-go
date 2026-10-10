package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscape(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"a,b", `"a,b"`},
		{`say "hi"`, `"say ""hi"""`},
		{"line\nbreak", "\"line\nbreak\""},
		{"cr\rhere", "\"cr\rhere\""},
		{"", ""},
	}
	for _, c := range cases {
		if got := Escape(c.in); got != c.want {
			t.Fatalf("Escape(%q) 期望 %q，实际 %q", c.in, c.want, got)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")

	rows := [][]string{
		{"1", "温度", "12.5"},
		{"2", "压力, 表压", "3.0"},
		{"3", `带"引号"的值`, "0"},
	}
	if err := WriteCSV(path, []string{"序号", "名称", "数值"}, rows); err != nil {
		t.Fatalf("写出失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	text := string(data)

	// 必须有 UTF-8 BOM，否则 Excel 打开中文乱码
	if !strings.HasPrefix(text, string(BOM)) {
		t.Fatal("缺少 UTF-8 BOM")
	}
	// 行尾必须是 CRLF（含表头与每个数据行）
	if !strings.Contains(text, "名称,数值\r\n") {
		t.Fatal("表头行尾应为 CRLF")
	}
	if !strings.Contains(text, "温度,12.5\r\n") {
		t.Fatal("数据行尾应为 CRLF")
	}
	if strings.Contains(text, "\n\n") {
		t.Fatal("不应出现 LF 与 CRLF 混用")
	}
	if !strings.Contains(text, `"压力, 表压"`) {
		t.Fatal("含逗号的字段应加引号")
	}
	if !strings.Contains(text, `"带""引号""的值"`) {
		t.Fatal("引号应翻倍转义")
	}
}
