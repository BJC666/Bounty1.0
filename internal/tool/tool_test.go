package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type testTool struct {
	name string
	ro   bool
}

func (t testTool) Name() string                           { return t.name }
func (t testTool) Description() string                    { return "test" }
func (t testTool) Schema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (t testTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "ok", nil
}
func (t testTool) ReadOnly() bool { return t.ro }

func TestRegistryAddAndGet(t *testing.T) {
	reg := NewRegistry()
	reg.Add(testTool{name: "test1"})
	reg.Add(testTool{name: "test2"})
	if len(reg.All()) != 2 {
		t.Error("expected 2 tools")
	}
	if _, ok := reg.Get("test1"); !ok {
		t.Error("test1 not found")
	}
	if _, ok := reg.Get("nonexistent"); ok {
		t.Error("should not find")
	}
}

func TestRegistryRemove(t *testing.T) {
	reg := NewRegistry()
	reg.Add(testTool{name: "a"})
	reg.Add(testTool{name: "b"})
	reg.Remove("a")
	if len(reg.All()) != 1 {
		t.Error("expected 1 tool after remove")
	}
}

func TestRegistrySchemasSorted(t *testing.T) {
	reg := NewRegistry()
	reg.Add(testTool{name: "c"})
	reg.Add(testTool{name: "a"})
	reg.Add(testTool{name: "b"})
	schemas := reg.Schemas()
	if len(schemas) != 3 {
		t.Error("expected 3 schemas")
	}
}

func TestRegistryReadOnly(t *testing.T) {
	reg := NewRegistry()
	reg.Add(testTool{name: "rw", ro: false})
	reg.Add(testTool{name: "ro", ro: true})
	roTools := reg.ReadOnlyTools()
	if len(roTools) != 1 {
		t.Error("expected 1 read-only tool")
	}
	if roTools[0].Name() != "ro" {
		t.Error("wrong read-only tool")
	}
}

func TestRegistryOwnerOf(t *testing.T) {
	reg := NewRegistry()
	reg.Add(testTool{name: "t1"})
	owner := reg.OwnerOf("t1")
	if owner.Kind != "unknown" {
		t.Errorf("expected unknown, got %s", owner.Kind)
	}
}
