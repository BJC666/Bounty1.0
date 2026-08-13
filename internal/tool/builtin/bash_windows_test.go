package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// cmdActive reports whether the bash tool will use cmd.exe as its shell
// (sh missing from PATH), which is the environment these Windows tests target.
func cmdActive() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	_, err := exec.LookPath("sh")
	return err != nil
}

func TestPrecheckWindowsCommand(t *testing.T) {
	cases := []struct {
		cmd     string
		blocked bool
		hint    string
	}{
		{"ls", true, "dir"},
		{"ls -la", true, "dir"},
		{"pwd", true, "cd"},
		{"cat f.txt", true, "type"},
		{"cp a b", true, "copy"},
		{"mv a b", true, "move"},
		{"rm -rf x", true, "del"},
		{"grep foo bar", true, "findstr"},
		{"python -V", false, ""},
		{"go test ./...", false, ""},
		{"dir /b", false, ""},
		{"echo hi", false, ""},
		{"type f.txt", false, ""},
		{"git status", false, ""},
		{"npm run dev", false, ""},
		{"\"ls\" -la", true, "dir"},
		{"  cd ..", false, ""},
		{"set X=1", false, ""},
	}
	for _, c := range cases {
		hint, blocked := precheckWindowsCommand(c.cmd)
		if blocked != c.blocked {
			t.Fatalf("precheck(%q) blocked=%v, want %v", c.cmd, blocked, c.blocked)
		}
		if c.blocked && !strings.Contains(hint, c.hint) {
			t.Fatalf("precheck(%q) hint=%q, want contains %q", c.cmd, hint, c.hint)
		}
	}
}

func TestFirstCommandToken(t *testing.T) {
	cases := map[string]string{
		"ls -la":          "ls",
		`"ls" -la`:        "ls",
		`'cat' f`:         "cat",
		"  echo hi  ":     "echo",
		"":                "",
		"git status":      "git",
		"python -c \"x\"": "python",
		"D:\\x\\y":        "D:\\x\\y",
	}
	for in, want := range cases {
		if got := firstCommandToken(in); got != want {
			t.Fatalf("firstCommandToken(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestMaybeAppendWindowsHint(t *testing.T) {
	cnOut := "'ls' 不是内部或外部命令，也不是可运行的程序\n或批处理文件。"
	if got := maybeAppendWindowsHint("cmd", "ls -la", cnOut); !strings.Contains(got, "建议改用 dir") {
		t.Fatalf("cn alias hint missing: %q", got)
	}
	enOut := "'pwd' is not recognized as an internal or external command,\noperable program or batch file."
	if got := maybeAppendWindowsHint("cmd", "pwd", enOut); !strings.Contains(got, "建议改用 cd") {
		t.Fatalf("en alias hint missing: %q", got)
	}
	if got := maybeAppendWindowsHint("cmd", "nonexistent-cmd", enOut); !strings.Contains(got, "where nonexistent-cmd") {
		t.Fatalf("generic hint missing: %q", got)
	}
	if got := maybeAppendWindowsHint("sh", "ls", cnOut); got != cnOut {
		t.Fatalf("sh output must be untouched: %q", got)
	}
	if got := maybeAppendWindowsHint("cmd", "ls", "total 4"); got != "total 4" {
		t.Fatalf("success output must be untouched: %q", got)
	}
}

func TestPrepareCommandWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only path rewrite")
	}
	cases := []struct{ in, want string }{
		{`cd D:\文件夹\a`, `cd D:/文件夹/a`},
		{`type D:\智能体\f.txt`, `type D:/智能体/f.txt`},
		{`echo ok`, `echo ok`},
		{`copy "D:\a b\c" out`, `copy "D:\a b\c" out`},
		{`ls D:\x && echo done`, `ls D:/x && echo done`},
		{`python D:\proj\main.py arg`, `python D:/proj/main.py arg`},
		{`cd D:/already/slash`, `cd D:/already/slash`},
		{`echo "D:\keep\as-is"`, `echo "D:\keep\as-is"`},
		{`cmd /c dir D:\tmp`, `cmd /c dir D:/tmp`},
	}
	for _, c := range cases {
		if got := prepareCommand(c.in); got != c.want {
			t.Fatalf("prepareCommand(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecodeOutput(t *testing.T) {
	if got := decodeOutput([]byte("hello 中文")); got != "hello 中文" {
		t.Fatalf("utf8 passthrough: %q", got)
	}
	if runtime.GOOS == "windows" {
		gbk, _, err := transform.String(simplifiedchinese.GBK.NewEncoder(), "中文测试")
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeOutput([]byte(gbk)); got != "中文测试" {
			t.Fatalf("gbk decode: %q", got)
		}
	}
}

func TestTrimHeadTailBudget(t *testing.T) {
	if got := trimHeadTail("short", 100, 100); got != "short" {
		t.Fatalf("short passthrough: %q", got)
	}
	long := strings.Repeat("x", 1000) + "END"
	got := trimHeadTail(long, 100, 100)
	if !strings.Contains(got, "END") || !strings.Contains(got, "已保留头尾") && !strings.Contains(got, "truncated") {
		t.Fatalf("trim markers missing: %q", got)
	}
}

func runBash(t *testing.T, b *BashTool, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return b.Execute(context.Background(), raw)
}

// Windows live tests run only when the bash tool falls back to cmd.exe.
func TestWindowsLiveEcho(t *testing.T) {
	if !cmdActive() {
		t.Skip("cmd fallback not active")
	}
	out, err := runBash(t, &BashTool{}, map[string]any{"command": "echo hello-bounty", "description": "live echo"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello-bounty") {
		t.Fatalf("out=%q", out)
	}
}

func TestWindowsLiveDirAndFileOps(t *testing.T) {
	if !cmdActive() {
		t.Skip("cmd fallback not active")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := runBash(t, &BashTool{}, map[string]any{"command": "dir /b \"" + f + "\"", "description": "list temp file"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "probe.txt") {
		t.Fatalf("out=%q", out)
	}
	out, err = runBash(t, &BashTool{}, map[string]any{"command": "type \"" + f + "\"", "description": "read temp file"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "x") {
		t.Fatalf("out=%q", out)
	}
}

func TestWindowsLiveUnixCommandBlockedWithHint(t *testing.T) {
	if !cmdActive() {
		t.Skip("cmd fallback not active")
	}
	_, err := runBash(t, &BashTool{}, map[string]any{"command": "ls -la", "description": "unix-only command"})
	if err == nil {
		t.Fatal("ls must be blocked under cmd")
	}
	if !strings.Contains(err.Error(), "Windows 等价命令") || !strings.Contains(err.Error(), "dir") {
		t.Fatalf("hint missing: %v", err)
	}
}

func TestWindowsLiveTimeoutKillsCommand(t *testing.T) {
	if !cmdActive() {
		t.Skip("cmd fallback not active")
	}
	_, err := runBash(t, &BashTool{}, map[string]any{"command": "ping -n 30 127.0.0.1", "description": "long-running", "timeout": 1000})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWindowsLiveChinesePath(t *testing.T) {
	if !cmdActive() {
		t.Skip("cmd fallback not active")
	}
	dir := t.TempDir()
	cn := filepath.Join(dir, "中文目录")
	if err := os.Mkdir(cn, 0755); err != nil {
		t.Fatal(err)
	}
	cmdline := "cd /d \"" + cn + "\" && echo 中文内容 > 中文文件.txt && type 中文文件.txt"
	out, err := runBash(t, &BashTool{}, map[string]any{"command": cmdline, "description": "chinese path roundtrip"})
	if err != nil {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if !strings.Contains(out, "中文内容") {
		t.Fatalf("out=%q", out)
	}
}
