package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// driftCase is one external-drift scenario: the file drifted to drifted
// after the model captured old_string from the original content; the retry
// must still succeed.
type driftCase struct {
	name     string
	original string // content the model saw when capturing old_string
	drifted  string // content after external modification
	old      string
	new      string
}

var driftCases = []driftCase{
	{"tabs-to-spaces", "func f() {\n\treturn 1\n}", "func f() {\n    return 1\n}", "\treturn 1", "    return 2"},
	{"spaces-to-tabs", "func f() {\n    return 1\n}", "func f() {\n\treturn 1\n}", "    return 1", "\treturn 2"},
	{"extra-indent", "func f() {\n\tif x {\n\t\ty()\n\t}\n}", "func f() {\n\t\tif x {\n\t\t\ty()\n\t\t}\n}", "\tif x {\n\t\ty()\n\t}", "\tif x {\n\t\tz()\n\t}"},
	{"reduced-indent", "func f() {\n\t\tif x {\n\t\t\ty()\n\t\t}\n}", "func f() {\n\tif x {\n\t\ty()\n\t}\n}", "\t\tif x {\n\t\t\ty()\n\t\t}", "\tif x {\n\t\tz()\n\t}"},
	{"trailing-ws-added", "a := 1\nb := 2", "a := 1   \nb := 2\t", "a := 1", "a := 3"},
	{"trailing-ws-removed", "a := 1   \nb := 2\t", "a := 1\nb := 2", "a := 1   ", "a := 3"},
	{"crlf-file", "a\nb\nc", "a\r\nb\r\nc", "a\nb", "x\ny"},
	{"crlf-old", "a\r\nb\r\nc", "a\nb\nc", "a\r\nb", "x\ny"},
	{"blank-line-split", "line1\n\nline2", "line1\n\n\nline2", "line1\n\nline2", "line1\n\nchanged"},
	{"blank-line-joined", "line1\n\n\nline2", "line1\n\nline2", "line1\n\n\nline2", "line1\n\nchanged"},
	{"inner-spacing", "result := a + b", "result := a  +  b", "a + b", "a - b"},
	{"multi-line-mixed-drift", "def run(x):\n    return x\n", "def run(x):\n\t\treturn x  \n", "def run(x):\n    return x", "def run(x):\n\t\treturn x + 1"},
	{"block-surrounding-blanks", "start\n\nmid\n\nend", "\nstart\n\nmid\n\nend\n", "start\n\nmid", "start\n\nchanged"},
	{"go-struct-alignment", "type T struct {\n\tA int\n\tBB int\n}", "type T struct {\n\tA  int\n\tBB int\n}", "\tA int\n\tBB int", "\tA int64\n\tBB int"},
	{"comment-indent", "// note\nfunc f() {}", "  // note\nfunc f() {}", "// note", "// fixed note"},
	{"inline-comment", "x := 1 // one", "x := 1  // one", "1 // one", "2 // one"},
	{"py-def-spacing", "def f( a , b ):\n    pass", "def f(a, b):\n        pass", "def f( a , b ):\n    pass", "def f(a, b):\n        return a"},
	{"five-line-block", "one\ntwo\nthree\nfour\nfive", "one  \n\ttwo\n\tthree\n\t four\nfive\t", "one\ntwo\nthree\nfour", "1\n2\n3\n4"},
	{"sql-ish-list", "VALUES (1, 2, 3)", "VALUES (1,  2,   3)", "1, 2, 3", "9, 9, 9"},
	{"markdown-heading", "# Title\n\ntext here", "#   Title\n\ntext  here", "# Title", "# New Title"},
}

func edit(t *testing.T, filePath string, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return (EditFileTool{}).Execute(context.Background(), raw)
}

func writeDrift(t *testing.T, c driftCase) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte(c.drifted), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEditFileDriftRetry(t *testing.T) {
	passed := 0
	for _, c := range driftCases {
		t.Run(c.name, func(t *testing.T) {
			p := writeDrift(t, c)
			out, err := edit(t, p, map[string]any{"file_path": p, "old_string": c.old, "new_string": c.new})
			if err != nil {
				t.Fatalf("drift retry failed: %v", err)
			}
			data, _ := os.ReadFile(p)
			if !strings.Contains(string(data), c.new) {
				t.Fatalf("new_string missing after edit:\n%s\noutput=%s", string(data), out)
			}
			passed++
		})
	}
	if passed < 19 {
		t.Fatalf("drift success %d/20, below 95%%", passed)
	}
}

func TestEditFileExactPathStillWorks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := edit(t, p, map[string]any{"file_path": p, "old_string": "hello", "new_string": "goodbye"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Replaced 1 occurrence") {
		t.Errorf("out=%s", out)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "goodbye world") {
		t.Fatalf("content=%s", string(data))
	}
}

func TestEditFileNonUniqueErrorListsLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("dup here\nmid\ndup here\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := edit(t, p, map[string]any{"file_path": p, "old_string": "dup here", "new_string": "x"})
	if err == nil {
		t.Fatal("expected non-unique error")
	}
	if !strings.Contains(err.Error(), "2 次") || !strings.Contains(err.Error(), "第 1 行") {
		t.Errorf("error missing guidance: %v", err)
	}
}

func TestEditFileNormalizedMultiMatchRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("alpha\nother\nalpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := edit(t, p, map[string]any{"file_path": p, "old_string": "alpha", "new_string": "x"})
	if err == nil {
		t.Fatal("expected non-unique error")
	}
}

func TestEditFileContextWindowOnHardMiss(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "line-" + strings.Repeat("x", i%7)
	}
	lines[30] = "target-ish content"
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := edit(t, p, map[string]any{"file_path": p, "old_string": "completely absent", "new_string": "x", "context_lines": 10})
	if err == nil {
		t.Fatal("expected miss error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "第 31 行") {
		t.Errorf("best-guess line missing: %s", msg[:300])
	}
	if !strings.Contains(msg, "target-ish content") {
		t.Errorf("context content missing: %s", msg[:400])
	}
	if !strings.Contains(msg, "重试") {
		t.Errorf("retry hint missing: %s", msg[:200])
	}
}

func TestEditFileReplaceAllExact(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("a a a"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := edit(t, p, map[string]any{"file_path": p, "old_string": "a", "new_string": "b", "replace_all": true})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "b b b" {
		t.Fatalf("content=%q", string(data))
	}
	_ = out
}

func writeArgs(t *testing.T, args map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWriteFileOverwriteGuard(t *testing.T) {
	p := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(p, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := (WriteFileTool{}).Execute(context.Background(), writeArgs(t, map[string]any{"file_path": p, "content": "new"}))
	if err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("expected overwrite guard error, got %v", err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "original" {
		t.Fatal("file must not be overwritten")
	}

	_, err = (WriteFileTool{}).Execute(context.Background(), writeArgs(t, map[string]any{"file_path": p, "content": "new", "overwrite": true}))
	if err != nil {
		t.Fatalf("overwrite:true must succeed: %v", err)
	}
	data, _ = os.ReadFile(p)
	if string(data) != "new" {
		t.Fatalf("content=%q", string(data))
	}
}

func TestWriteFileNewFileNoOverwriteNeeded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new.txt")
	if _, err := (WriteFileTool{}).Execute(context.Background(), writeArgs(t, map[string]any{"file_path": p, "content": "fresh"})); err != nil {
		t.Fatal(err)
	}
}
