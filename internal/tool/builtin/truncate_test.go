package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tailOf returns the last n runes of s.
func tailOf(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func makeLinesFile(t *testing.T, n int) string {
	t.Helper()
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		sb.WriteString(fmt.Sprintf("line %d content\n", i))
	}
	p := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(p, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFileDefault2000LineBudget(t *testing.T) {
	p := makeLinesFile(t, 2500)
	out, err := (ReadFileTool{}).Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q}`, p)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "line 1 content") {
		t.Error("head missing")
	}
	if strings.Contains(out, "line 2500 content") {
		t.Error("tail must be cut")
	}
	if !strings.Contains(out, "共 2500 行") || !strings.Contains(out, "第 1–2000 行") || !strings.Contains(out, "offset=2000") {
		t.Errorf("missing truncation guidance: %s", tailOf(out, 400))
	}
}

func TestReadFileExplicitLimitNoNotice(t *testing.T) {
	p := makeLinesFile(t, 2500)
	out, err := (ReadFileTool{}).Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"limit":100}`, p)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "共 2500 行") || !strings.Contains(out, "第 1–100 行") || !strings.Contains(out, "offset=100") {
		t.Errorf("explicit limit must include continuation guidance: %s", tailOf(out, 200))
	}
	if !strings.Contains(out, "line 100 content") {
		t.Error("limit content missing")
	}
}

func TestReadFileOffsetContinuation(t *testing.T) {
	p := makeLinesFile(t, 2500)
	out, err := (ReadFileTool{}).Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"offset":2000}`, p)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "line 2001 content") {
		t.Errorf("offset content missing: %s", out[:200])
	}
	if !strings.Contains(out, "line 2500 content") {
		t.Error("full tail must be shown when within budget")
	}
	if strings.Contains(out, "truncated") {
		t.Errorf("within budget must not truncate: %s", tailOf(out, 200))
	}
}

func TestReadFileOffsetBeyondEnd(t *testing.T) {
	p := makeLinesFile(t, 10)
	out, err := (ReadFileTool{}).Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"file_path":%q,"offset":9999}`, p)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "超出文件范围") {
		t.Errorf("unexpected: %s", out)
	}
}

func TestTrimHeadTailKeepsBorders(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40000; i++ {
		sb.WriteString(fmt.Sprintf("L%05d\n", i))
	}
	out := trimHeadTail(sb.String(), 100, 100)
	if !strings.Contains(out, "L00000") {
		t.Error("head missing")
	}
	if !strings.Contains(out, "L39999") {
		t.Error("tail missing")
	}
	if !strings.Contains(out, "bash output truncated") {
		t.Error("notice missing")
	}
	if strings.Contains(out, "L20000") {
		t.Error("middle must be elided")
	}
}

func TestTrimHeadTailShortUnchanged(t *testing.T) {
	s := "hello world"
	if got := trimHeadTail(s, 100, 100); got != s {
		t.Fatalf("changed: %q", got)
	}
}

func TestCapMatchLines(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 350; i++ {
		sb.WriteString(fmt.Sprintf("a.go:%d: match\n", i))
	}
	out := capMatchLines(sb.String())
	if strings.Contains(out, "a.go:349:") {
		t.Error("must cap at 300")
	}
	if !strings.Contains(out, "a.go:299:") {
		t.Error("line 300 missing")
	}
	if !strings.Contains(out, "共 350 条匹配") {
		t.Errorf("count notice missing: %s", tailOf(out, 200))
	}
}

func TestCapMatchLinesUnderLimitUnchanged(t *testing.T) {
	s := "a.go:1: x\na.go:2: y\n"
	if got := capMatchLines(s); got != s {
		t.Fatalf("changed: %q", got)
	}
}

func TestGrepFallbackCap(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		sb.WriteString(fmt.Sprintf("needle%d\n", i))
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	params := struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}{Pattern: "needle", Path: dir}
	out, err := grepFallback(params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "共 400 条匹配") {
		t.Errorf("fallback cap notice missing: %s", tailOf(out, 300))
	}
	if strings.Contains(out, "needle399") {
		t.Error("fallback must cap")
	}
}
