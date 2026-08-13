package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSpecsMergeProjectOverridesUser(t *testing.T) {
	dir := t.TempDir()
	user := writeFile(t, dir, "bounty-data/mcp.json", `{
		"servers": [
			{"name": "shared", "command": "npx", "args": ["-y", "@user/shared"], "read_only": true},
			{"name": "useronly", "command": "python", "args": ["user.py"], "read_only": true},
			{"name": "sse", "url": "http://localhost:8080"}
		]
	}`)
	proj := writeFile(t, dir, ".bounty/mcp.json", `{
		"servers": [
			{"name": "shared", "command": "npx", "args": ["-y", "@project/shared"], "trust": true},
			{"name": "projectonly", "command": "python", "args": ["proj.py"]}
		]
	}`)
	missing := filepath.Join(dir, "nonexistent.json")

	specs, err := LoadSpecs(user, proj)
	if err != nil {
		t.Fatalf("LoadSpecs: %v", err)
	}
	byName := map[string]Spec{}
	var order []string
	for _, s := range specs {
		if _, dup := byName[s.Name]; dup {
			t.Fatalf("duplicate server %q", s.Name)
		}
		byName[s.Name] = s
		order = append(order, s.Name)
	}
	if len(specs) != 4 {
		t.Fatalf("want 4 servers, got %d (%v)", len(specs), order)
	}
	shared := byName["shared"]
	if len(shared.Args) != 2 || shared.Args[1] != "@project/shared" {
		t.Fatalf("project must override user by name: %+v", shared)
	}
	if shared.Trust != true {
		t.Fatalf("project trust field lost: %+v", shared)
	}
	if !byName["useronly"].ReadOnly {
		t.Fatal("useronly read_only lost")
	}
	if byName["sse"].URL != "http://localhost:8080" {
		t.Fatal("SSE URL lost")
	}
	// User servers keep their position; project servers are appended.
	wantOrder := []string{"shared", "useronly", "sse", "projectonly"}
	for i, n := range wantOrder {
		if order[i] != n {
			t.Fatalf("order[%d]=%q, want %q (%v)", i, order[i], n, order)
		}
	}

	// Missing files are fine.
	if specs, err := LoadSpecs(missing, missing); err != nil || len(specs) != 0 {
		t.Fatalf("missing files: specs=%v err=%v", specs, err)
	}
}

func TestLoadSpecsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad/mcp.json", `{"servers": [`)
	if _, err := LoadSpecs(bad, filepath.Join(dir, "nope")); err == nil {
		t.Fatal("expected parse error")
	}
}
