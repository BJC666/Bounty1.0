package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/memory"
)

func TestMemorySearchToolReadOnlyAndOwner(t *testing.T) {
	ms := &MemorySearchTool{ProjectRoot: t.TempDir()}
	if !ms.ReadOnly() {
		t.Error("memory_search must be read-only")
	}
	if ms.Name() != "memory_search" {
		t.Errorf("name=%s", ms.Name())
	}
}

func TestMemorySearchToolFindsSavedEntry(t *testing.T) {
	root := t.TempDir()
	store := memory.NewRememberStore(root)
	if err := store.Save("naming-convention", "变量命名偏好", "用户偏好：变量命名一律使用 snake_case"); err != nil {
		t.Fatal(err)
	}

	ms := &MemorySearchTool{ProjectRoot: root}
	args := json.RawMessage(`{"query":"变量命名"}`)
	out, err := ms.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "naming-convention") {
		t.Errorf("missing name in output: %s", out)
	}
	if !strings.Contains(out, "snake_case") {
		t.Errorf("missing content in output: %s", out)
	}
	if !strings.Contains(out, "Found 1 memories") {
		t.Errorf("unexpected header: %s", out)
	}
}

func TestMemorySearchToolEmptyQueryListsRecent(t *testing.T) {
	root := t.TempDir()
	store := memory.NewRememberStore(root)
	if err := store.Save("a-note", "备注", "内容甲"); err != nil {
		t.Fatal(err)
	}

	ms := &MemorySearchTool{ProjectRoot: root}
	out, err := ms.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a-note") {
		t.Errorf("recent list missing entry: %s", out)
	}
}

func TestMemorySearchToolNoMatch(t *testing.T) {
	ms := &MemorySearchTool{ProjectRoot: t.TempDir()}
	out, err := ms.Execute(context.Background(), json.RawMessage(`{"query":"不存在的关键词xyz"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No matching memories") {
		t.Errorf("out=%s", out)
	}
}

func TestMemorySearchToolLimitCapped(t *testing.T) {
	root := t.TempDir()
	store := memory.NewRememberStore(root)
	for _, n := range []string{"a", "b", "c"} {
		if err := store.Save(n, "d", "e"); err != nil {
			t.Fatal(err)
		}
	}
	ms := &MemorySearchTool{ProjectRoot: root}
	out, err := ms.Execute(context.Background(), json.RawMessage(`{"query":"d","limit":999}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Found 3 memories") {
		t.Errorf("capped limit should still return all 3: %s", out)
	}
}
