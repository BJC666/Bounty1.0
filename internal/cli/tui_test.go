package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"bounty/internal/store"
)

func TestParseSlash(t *testing.T) {
	cases := []struct {
		in  string
		cmd string
		arg string
		ok  bool
	}{
		{in: "/status", cmd: "/status", arg: "", ok: true},
		{in: "/model qwen/qwen3.8-max", cmd: "/model", arg: "qwen/qwen3.8-max", ok: true},
		{in: "/export 导出.md", cmd: "/export", arg: "导出.md", ok: true},
		{in: "普通消息", ok: false},
		{in: "不是 / 开头", ok: false},
		{in: "/", cmd: "/", arg: "", ok: true},
	}
	for _, tc := range cases {
		cmd, arg, ok := parseSlash(tc.in)
		if ok != tc.ok || cmd != tc.cmd || arg != tc.arg {
			t.Fatalf("parseSlash(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.in, cmd, arg, ok, tc.cmd, tc.arg, tc.ok)
		}
	}
}

func TestDiffLinesInsertion(t *testing.T) {
	lines := diffLines("a\nb\nc\n", "a\nb\nx\nc\n")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "+x") || strings.Contains(joined, "-") {
		t.Fatalf("insertion diff wrong:\n%s", joined)
	}
}

func TestDiffLinesDeletion(t *testing.T) {
	lines := diffLines("a\nb\nc\n", "a\nc\n")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "-b") || strings.Contains(joined, "+") {
		t.Fatalf("deletion diff wrong:\n%s", joined)
	}
}

func TestDiffLinesModification(t *testing.T) {
	lines := diffLines("hello world\nkeep\n", "hello bounty\nkeep\n")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "-hello world") || !strings.Contains(joined, "+hello bounty") {
		t.Fatalf("modification diff wrong:\n%s", joined)
	}
}

func TestDiffLinesIdentical(t *testing.T) {
	lines := diffLines("same\nsame2\n", "same\nsame2\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
			t.Fatalf("identical input produced diff line %q", l)
		}
	}
}

func TestDiffLinesEmptyOld(t *testing.T) {
	lines := diffLines("", "new file\n")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "+new file") {
		t.Fatalf("empty-old diff wrong:\n%s", joined)
	}
}

func TestTuiAskerRoundtrip(t *testing.T) {
	a := &tuiAsker{}
	done := make(chan string, 1)
	go func() {
		ans, err := a.Ask(context.Background(), "允许执行 bash 吗？", []string{"approve", "deny"})
		if err != nil {
			done <- "err:" + err.Error()
			return
		}
		done <- ans
	}()
	deadline := time.Now().Add(2 * time.Second)
	for a.Pending() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	p := a.Pending()
	if p == nil {
		t.Fatal("pending ask not set")
	}
	if !strings.Contains(p.question, "bash") || len(p.options) != 2 {
		t.Fatalf("pending = %+v", p)
	}
	if !a.Answer(0) {
		t.Fatal("answer rejected")
	}
	if ans := <-done; ans != "approve" {
		t.Fatalf("ans = %q, want approve", ans)
	}
}

func TestTuiAskerCancel(t *testing.T) {
	a := &tuiAsker{}
	done := make(chan string, 1)
	go func() { ans, _ := a.Ask(context.Background(), "q", []string{"yes"}); done <- ans }()
	for a.Pending() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	a.Cancel()
	if ans := <-done; ans != "" {
		t.Fatalf("ans = %q, want empty on cancel", ans)
	}
	if a.Pending() != nil {
		t.Fatal("pending should clear after cancel")
	}
}

func TestTuiAskerAnswerOutOfRange(t *testing.T) {
	a := &tuiAsker{}
	done := make(chan string, 1)
	go func() { ans, _ := a.Ask(context.Background(), "q", []string{"yes"}); done <- ans }()
	for a.Pending() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if a.Answer(5) {
		t.Fatal("out-of-range answer must be rejected")
	}
	a.Answer(0)
	if ans := <-done; ans != "yes" {
		t.Fatalf("ans = %q", ans)
	}
}

func TestTuiAskerCtxTimeout(t *testing.T) {
	a := &tuiAsker{}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := a.Ask(ctx, "q", []string{"yes"}); err == nil {
		t.Fatal("want ctx deadline error")
	}
}

func TestRenderToolPanelCollapsed(t *testing.T) {
	m := &tuiModel{width: 80}
	h := histEntry{kind: "tool", toolName: "edit_file",
		toolArgs:   `{"file_path":"a.txt","old_string":"hello","new_string":"world"}`,
		toolResult: "done ✓", expanded: false}
	lines := m.renderToolLines(h, 70)
	if len(lines) != 1 || !strings.Contains(lines[0], "edit_file") {
		t.Fatalf("collapsed lines = %q", lines)
	}
	if strings.Contains(lines[0], "hello") {
		t.Fatalf("collapsed line leaks args: %q", lines[0])
	}
}

func TestRenderToolPanelExpandedWithDiff(t *testing.T) {
	m := &tuiModel{width: 80}
	h := histEntry{kind: "tool", toolName: "edit_file",
		toolArgs:   `{"file_path":"a.txt","old_string":"hello","new_string":"world"}`,
		toolResult: "done ✓", expanded: true}
	joined := strings.Join(m.renderToolLines(h, 70), "\n")
	for _, want := range []string{"edit_file", "done ✓", "-hello", "+world"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expanded panel missing %q:\n%s", want, joined)
		}
	}
}

func TestRenderToolPanelExpandedNoDiffForBash(t *testing.T) {
	m := &tuiModel{width: 80}
	h := histEntry{kind: "tool", toolName: "bash",
		toolArgs:   `{"command":"go test ./..."}`,
		toolResult: "ok", expanded: true}
	joined := strings.Join(m.renderToolLines(h, 70), "\n")
	if !strings.Contains(joined, "go test") || !strings.Contains(joined, "ok") {
		t.Fatalf("bash panel:\n%s", joined)
	}
}

func TestBuildSessionMarkdown(t *testing.T) {
	sess := &store.Session{ID: "s1", Title: "测试会话", Model: "m", Provider: "p", CreatedAt: time.Now().Unix()}
	msgs := []store.Message{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "你好！"},
		{Role: "tool", ToolName: "read_file", Content: "文件内容"},
	}
	md := buildSessionMarkdown(sess, msgs)
	for _, want := range []string{"# 测试会话", "**You:** 你好", "**Bounty:** 你好！", "🔧 read_file"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
