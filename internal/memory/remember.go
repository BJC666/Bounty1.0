package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry is a single auto-memory entry.
type Entry struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}

// RememberStore manages per-project auto-memory files.
type RememberStore struct {
	dir   string // .agent/memory/
	index string // MEMORY.md
}

func NewRememberStore(projectRoot string) *RememberStore {
	dir := filepath.Join(projectRoot, ".agent", "memory")
	return &RememberStore{
		dir:   dir,
		index: filepath.Join(dir, "MEMORY.md"),
	}
}

// Save persists a memory entry as a frontmatter .md file.
func (rs *RememberStore) Save(name, description, content string) error {
	if err := os.MkdirAll(rs.dir, 0755); err != nil {
		return err
	}

	filename := strings.ToLower(strings.ReplaceAll(name, " ", "-")) + ".md"
	path := filepath.Join(rs.dir, filename)

	entry := fmt.Sprintf(`---
name: %s
description: %s
created: %s
---

%s
`, name, description, time.Now().Format(time.RFC3339), content)

	if err := os.WriteFile(path, []byte(entry), 0644); err != nil {
		return err
	}

	// Update index
	return rs.rebuildIndex()
}

// rebuildIndex regenerates MEMORY.md from all .md files.
func (rs *RememberStore) rebuildIndex() error {
	entries, err := os.ReadDir(rs.dir)
	if err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("# Auto-Memory Index\n\n")
	for _, e := range entries {
		if e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(rs.dir, e.Name()))
		content := string(data)
		// Extract frontmatter name
		name := strings.TrimSuffix(e.Name(), ".md")
		if strings.HasPrefix(content, "---\n") {
			lines := strings.Split(content, "\n")
			for _, line := range lines[1:] {
				if strings.HasPrefix(line, "name: ") {
					name = strings.TrimPrefix(line, "name: ")
					break
				}
				if line == "---" {
					break
				}
			}
		}
		sb.WriteString(fmt.Sprintf("- [%s](%s)\n", name, e.Name()))
	}
	return os.WriteFile(rs.index, []byte(sb.String()), 0644)
}
