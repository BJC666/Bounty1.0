package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"My Memory":   "my-memory",
		"../evil":     "--evil",
		`a\b\c`:       "abc",
		"UPPER Case!": "upper-case",
		"":            "memory",
		"a.b.c":       "a-b-c",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRememberSaveRejectsTraversal(t *testing.T) {
	projectRoot := t.TempDir()
	rs := NewRememberStore(projectRoot)
	if err := rs.Save("../evil", "desc", "content"); err != nil {
		t.Fatal(err)
	}
	// The file must land inside the memory dir, not at the parent level.
	if _, err := os.Stat(filepath.Join(projectRoot, "evil.md")); err == nil {
		t.Fatal("traversal escaped the memory directory")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".agent", "memory", "--evil.md")); err != nil {
		t.Fatal("expected sanitized file inside memory dir")
	}
}

func TestRememberSaveEmptyName(t *testing.T) {
	rs := NewRememberStore(t.TempDir())
	if err := rs.Save("   ", "desc", "content"); err == nil {
		t.Fatal("expected error for empty name")
	}
}