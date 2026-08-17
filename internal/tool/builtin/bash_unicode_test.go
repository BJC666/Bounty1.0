//go:build windows

package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestIsASCII(t *testing.T) {
	cases := map[string]bool{
		"":                     true,
		"echo hello":           true,
		`python -c "print(1)"`: true,
		"echo 中文测试":            false,
		`cd /d "D:\文件夹"`:       false,
		"dir *.go":             true,
	}
	for in, want := range cases {
		if got := isASCII(in); got != want {
			t.Fatalf("isASCII(%q)=%v, want %v", in, got, want)
		}
	}
}

func TestWriteUTF8CmdFile(t *testing.T) {
	path, cleanup, err := writeUTF8CmdFile("echo 中文测试")
	if err != nil {
		t.Fatalf("writeUTF8CmdFile: %v", err)
	}
	defer cleanup()
	if !strings.HasSuffix(path, "run.cmd") {
		t.Fatalf("unexpected file name: %q", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if !utf8.Valid(b) {
		t.Fatalf("temp .cmd file is not valid UTF-8")
	}
	head := string(b)
	if !strings.HasPrefix(head, "@chcp 65001 >nul\r\n") {
		t.Fatalf("file must pin chcp 65001 first, got %q", head[:min(len(head), 40)])
	}
	if !strings.Contains(head, "中文测试") {
		t.Fatalf("file must carry the UTF-8 command, got %q", head)
	}
}

// TestBashNonASCIICmd runs BashTool.Execute with Chinese args/paths from a
// Chinese working directory (the repo root itself) — the exact scenario that
// previously produced "文件名、目录名或卷标语法不正确" / "=== === ===" /
// `FINDSTR: 无法打开` garbled outputs.
func TestBashNonASCIICmd(t *testing.T) {
	bt := &BashTool{Timeout: 30 * time.Second}

	run := func(command string) string {
		args, err := json.Marshal(map[string]any{"command": command, "description": "t"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := bt.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute(%q) err: %v\noutput: %s", command, err, out)
		}
		return out
	}

	if out := run("echo 中文测试"); !strings.Contains(out, "中文测试") {
		t.Fatalf("echo 中文测试 output missing 中文测试: %q", out)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if !isASCII(cwd) {
		// 中文工作目录 + 引号路径（此前 cmd /c 引号剥离 + ANSI 重编码的失败场景）
		if out := run(`cd /d "` + cwd + `" && echo OK`); !strings.Contains(out, "OK") {
			t.Fatalf("cd 中文路径 output missing OK: %q", out)
		}
	}

	// 引号包裹 + 中文参数（此前被 cmd /c 首引号剥离拆坏）
	if out := run(`echo "带引号中文参数"`); !strings.Contains(out, "带引号中文参数") {
		t.Fatalf("quoted Chinese args output missing: %q", out)
	}

	// 带引号可执行文件 + 中文代码参数（此前 cmd /c 把 "C:\...\python.exe" -c "中文" 拆成
	// `'C:\...\python.exe" -c "print' is not recognized`）。python 不在 PATH 时跳过。
	if py, err := exec.LookPath("python"); err == nil {
		cmd := `"` + py + `" -c "print('中文测试')"`
		if out := run(cmd); !strings.Contains(out, "中文测试") {
			t.Fatalf("python -c 中文 output missing: %q", out)
		}
	}
}
