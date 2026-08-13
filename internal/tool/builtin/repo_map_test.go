package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bounty/internal/repomap"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCodeIndexDelegatesToRepomap(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", "package pkg\n\ntype Widget struct{}\n\nfunc Make() *Widget { return nil }\n")

	args, _ := json.Marshal(map[string]string{"path": filepath.ToSlash(filepath.Join(root, "pkg")), "kind": "all"})
	out, err := (CodeIndexTool{}).Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[type] Widget") {
		t.Errorf("missing type symbol: %s", out)
	}
	if !strings.Contains(out, "[function] Make") {
		t.Errorf("missing function symbol: %s", out)
	}
}

func TestRepoMapToolReadOnly(t *testing.T) {
	rt := NewRepoMapTool(repomap.NewManager(t.TempDir()))
	if !rt.ReadOnly() {
		t.Error("repo_map must be read-only")
	}
	if rt.Name() != "repo_map" {
		t.Errorf("name=%s", rt.Name())
	}
}

func TestRepoMapToolReturnsBlock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "lib/util.py", "def helper():\n    return 1\n")

	rt := NewRepoMapTool(repomap.NewManager(root))
	out, err := rt.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## Repo Map") || !strings.Contains(out, "helper") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestRepoMapToolForcesRefresh(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "one.go", "package one\n\nfunc First() {}\n")
	rt := NewRepoMapTool(repomap.NewManager(root))

	out1, _ := rt.Execute(context.Background(), json.RawMessage(`{}`))
	writeFile(t, root, "two.go", "package two\n\nfunc Second() {}\n")
	out2, _ := rt.Execute(context.Background(), json.RawMessage(`{}`))

	if strings.Contains(out1, "Second") {
		t.Error("first render must not contain Second")
	}
	if !strings.Contains(out2, "Second") {
		t.Errorf("refresh must include Second: %s", out2)
	}
}
