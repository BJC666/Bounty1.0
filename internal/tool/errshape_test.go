package tool

import (
	"errors"
	"strings"
	"testing"
)

func TestShapeErrorThreeLines(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		err     error
		wantCat string
	}{
		{"arg-invalid", "edit_file", errors.New(`json: cannot unmarshal number into Go struct field .file_path of type string`), "参数错误"},
		{"file-missing", "read_file", errors.New("open C:\\x\\y.txt: The system cannot find the file specified."), "文件不存在"},
		{"permission", "bash", errors.New("command denied by permission policy"), "权限拒绝"},
		{"non-unique", "edit_file", errors.New("old_string 在 f.txt 中出现 2 次（不唯一）"), "匹配不唯一"},
		{"no-match", "edit_file", errors.New("old_string 未找到（精确匹配与空白归一化匹配均未命中）"), "匹配未命中"},
		{"timeout", "bash", errors.New("command timed out after 60s"), "超时"},
		{"network", "web_fetch", errors.New("refusing to fetch non-public address: 127.0.0.1:8765"), "网络错误"},
		{"unknown", "nope", errors.New("unknown tool: nope"), "未知工具"},
		{"encoding", "bash", errors.New("文件名、目录名或卷标语法不正确。"), "路径/编码错误"},
		{"fallback", "bash", errors.New("something very unusual happened"), "其他错误"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := ShapeError(c.tool, c.err)
			if !strings.Contains(out, "【错误类型】"+c.wantCat) {
				t.Fatalf("category mismatch:\n%s", out)
			}
			if !strings.Contains(out, "【原因】") || !strings.Contains(out, "【建议重试】") {
				t.Fatalf("missing lines:\n%s", out)
			}
			if !strings.Contains(out, "重试") {
				t.Fatalf("no retry hint:\n%s", out)
			}
		})
	}
}

func TestShapeErrorTruncatesLongReason(t *testing.T) {
	long := strings.Repeat("x", 1200)
	out := ShapeError("bash", errors.New(long))
	if strings.Contains(out, strings.Repeat("x", 1200)) {
		t.Fatalf("reason not truncated")
	}
	if !strings.Contains(out, "(截断)") {
		t.Fatalf("truncation marker missing")
	}
}
