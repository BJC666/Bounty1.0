package repomap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRenderTreeSymbolsAndDeps(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/repo\n\ngo 1.22\n")
	write(t, root, "internal/store/store.go", `package store

import (
	"fmt"

	"example.com/repo/internal/model"
)

// Store persists data.
type Store struct{}

func New() *Store { return &Store{} }
`)
	write(t, root, "internal/model/model.go", "package model\n\ntype Todo struct {\n\tID int\n}\n")
	write(t, root, "stats/core.py", "import os\nfrom internal_helper import x\n\ndef median(v):\n    return sorted(v)\n")
	write(t, root, "node_modules/evil.js", "function evil() {}\n")

	m := NewManager(root)
	block := m.Render()
	if block == "" {
		t.Fatal("empty repo map")
	}
	if !strings.Contains(block, "## Repo Map") {
		t.Error("missing header")
	}
	if !strings.Contains(block, "[type]Store") {
		t.Errorf("missing Go type symbol:\n%s", block)
	}
	if !strings.Contains(block, "[function]median") {
		t.Errorf("missing Python symbol:\n%s", block)
	}
	if !strings.Contains(block, "internal/store") {
		t.Errorf("missing dir section:\n%s", block)
	}
	if strings.Contains(block, "node_modules") {
		t.Error("node_modules must be skipped")
	}
	// Go internal dependency via module path
	if !strings.Contains(block, "example.com/repo/internal/model") {
		t.Errorf("missing internal dep edge:\n%s", block)
	}
}

func TestRefreshDetectsChange(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package a\n\nfunc Hello() {}\n")
	m := NewManager(root)

	block1, changed1 := m.Refresh()
	if !changed1 || !strings.Contains(block1, "Hello") {
		t.Fatalf("first refresh should build: changed=%v", changed1)
	}
	_, changed2 := m.Refresh()
	if changed2 {
		t.Fatal("unchanged tree must not rebuild")
	}

	time.Sleep(10 * time.Millisecond)
	write(t, root, "b.go", "package b\n\nfunc World() {}\n")
	block3, changed3 := m.Refresh()
	if !changed3 {
		t.Fatal("new file must trigger rebuild")
	}
	if !strings.Contains(block3, "World") {
		t.Errorf("rebuild missing new symbol:\n%s", block3)
	}
}

func TestRenderBudgetTruncates(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		sb.WriteString("func FunctionNameThatIsQuiteLong")
		sb.WriteString(strings.Repeat("x", 40))
		sb.WriteString("() {}\n")
	}
	write(t, root, "big.go", "package big\n"+sb.String())

	m := NewManager(root)
	block := m.Render()
	runes := []rune(block)
	if len(runes) > DefaultMaxRunes+64 {
		t.Fatalf("rendered %d runes, budget %d", len(runes), DefaultMaxRunes)
	}
}

func TestDependencyClassificationPythonRelative(t *testing.T) {
	root := t.TempDir()
	write(t, root, "app/main.py", "from .helper import h\nimport os\nimport external_lib\n")
	write(t, root, "app/helper.py", "def h():\n    pass\n")

	m := NewManager(root)
	block := m.Render()
	if !strings.Contains(block, ".helper") {
		t.Errorf("missing relative internal dep:\n%s", block)
	}
	if strings.Contains(block, "external_lib") {
		t.Errorf("external dep must not appear:\n%s", block)
	}
}

func TestEmptyTreeRendersNothing(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	if block := m.Render(); block != "" {
		t.Fatalf("expected empty block, got %q", block)
	}
}

func TestIndexFileCap(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.py", "def f1():\n    pass\ndef f2():\n    pass\ndef f3():\n    pass\n")
	syms := IndexFile(filepath.Join(root, "a.py"), "all", 2)
	if len(syms) != 2 {
		t.Fatalf("cap failed: %d", len(syms))
	}
}

// P8-2: boosted files must be rendered before un-boosted ones, in priority
// order (usage-ranked repo maps). Both SetBoost and the on-disk boost file
// are covered.
func TestBoostOrdering(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/repo\n\ngo 1.22\n")
	write(t, root, "internal/store/store.go", "package store\n\ntype Store struct{}\n")
	write(t, root, "internal/format/format.go", "package format\n\nfunc FormatList() {}\n")
	write(t, root, "main.go", "package main\n\nfunc main() {}\n")

	// 1. Programmatic boost: hot files first.
	m := NewManager(root)
	m.SetBoost([]string{"internal/format/format.go", "internal/store/store.go"})
	block := m.Render()
	idxFormat := strings.Index(block, "format.go")
	idxStore := strings.Index(block, "store.go")
	idxMain := strings.Index(block, "main.go")
	if idxFormat < 0 || idxStore < 0 || idxMain < 0 {
		t.Fatalf("missing entries:\n%s", block)
	}
	if !(idxFormat < idxStore && idxStore < idxMain) {
		t.Errorf("boost order violated (want format < store < main):\n%s", block)
	}

	// 2. On-disk boost file is honoured by NewManager.
	write(t, root, ".bounty/repomap-boost.json", `{"order":["main.go","internal/format/format.go"]}`)
	m2 := NewManager(root)
	block2 := m2.Render()
	iMain := strings.Index(block2, "main.go")
	iFormat := strings.Index(block2, "format.go")
	iStore := strings.Index(block2, "store.go")
	if !(iMain >= 0 && iFormat >= 0 && iStore >= 0) {
		t.Fatalf("missing entries:\n%s", block2)
	}
	if !(iMain < iFormat && iFormat < iStore) {
		t.Errorf("disk boost order violated (want main < format < store):\n%s", block2)
	}
}

// P8-3: Go receivers and TS annotated functions must render ONCE (function
// and method entries at the same line collapse to the preferred kind).
func TestRenderDedupesFunctionMethodPairs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/repo\n\ngo 1.22\n")
	write(t, root, "store.go", "package main\n\nfunc (s *Store) Add(id int) {}\nfunc New() {}\n")
	write(t, root, "util.ts", "export function truncate(text: string, max: number): string { return text; }\n")
	m := NewManager(root)
	block := m.Render()
	if strings.Count(block, "Add") != 1 {
		t.Errorf("Add must render once, got:\n%s", block)
	}
	if strings.Count(block, "truncate") != 1 {
		t.Errorf("truncate must render once, got:\n%s", block)
	}
	if !strings.Contains(block, "[method]Add") {
		t.Errorf("preferred kind for receiver should be method:\n%s", block)
	}
}
