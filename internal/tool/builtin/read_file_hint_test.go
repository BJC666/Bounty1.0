package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFilePathHints(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "src", "src", "dates.ts")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("export const d = 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Model guesses the wrong nesting (src/dates.ts instead of src/src/dates.ts).
	wrong := filepath.Join(dir, "src", "dates.ts")
	tool := ReadFileTool{}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"file_path":"`+filepath.ToSlash(wrong)+`"}`))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "疑似目标文件") || !strings.Contains(err.Error(), real) {
		t.Fatalf("hint missing: %v", err)
	}
}

func TestReadFileNoHintWhenNoCandidate(t *testing.T) {
	dir := t.TempDir()
	tool := ReadFileTool{}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"file_path":"`+filepath.ToSlash(filepath.Join(dir, "nope.go"))+`"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "疑似目标文件") {
		t.Fatalf("unexpected hint: %v", err)
	}
}
