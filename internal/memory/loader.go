package memory

import (
	"os"
	"path/filepath"
)

type Doc struct {
	Name    string
	Source  string // "project", "user", "global", "ancestor"
	Content string
	// InjectionHits lists prompt-injection / self-replication markers found
	// in the document. Empty when the document is clean.
	InjectionHits []string
}

// Load loads memory files from all levels.
// Priority: project > user > global > ancestor (latter do NOT override former).
func Load(projectRoot string) ([]Doc, error) {
	var docs []Doc

	// 4. Ancestor directories (monorepo scenario)
	dir := projectRoot
	for i := 0; i < 5; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if content, ok := readMemoryFile(filepath.Join(parent, "BOUNTY.md")); ok {
			docs = append(docs, Doc{Name: "ancestor", Source: "ancestor", Content: content})
		}
		dir = parent
	}

	// 3. User global
	home, _ := os.UserHomeDir()
	if content, ok := readMemoryFile(filepath.Join(home, ".config", "bounty", "BOUNTY.md")); ok {
		docs = append(docs, Doc{Name: "user", Source: "user", Content: content})
	}

	// 2. Project AGENTS.md (fallback)
	if content, ok := readMemoryFile(filepath.Join(projectRoot, "AGENTS.md")); ok {
		docs = append(docs, Doc{Name: "agents", Source: "project", Content: content})
	}

	// 1. Project BOUNTY.md (highest priority — appended last)
	if content, ok := readMemoryFile(filepath.Join(projectRoot, "BOUNTY.md")); ok {
		docs = append(docs, Doc{Name: "bounty", Source: "project", Content: content})
	}

	// Flag documents that carry injection / self-replication markers.
	for i := range docs {
		docs[i] = scanDoc(docs[i])
	}

	return docs, nil
}

func readMemoryFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	content := string(data)
	if len(content) > 32*1024 {
		content = content[:32*1024] + "\n... [truncated]"
	}
	return content, true
}

// scanDoc attaches any injection / self-replication markers found in the
// document content so callers can treat suspicious memory as data rather
// than instructions.
func scanDoc(d Doc) Doc {
	if hits := ScanAll(d.Content); len(hits) > 0 {
		d.InjectionHits = hits
	}
	return d
}
