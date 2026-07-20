package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCommandFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.md")
	content := `---
name: review
description: Review code changes
allowed-tools: [Read, Grep, Glob]
---

Review the changed files. Check for:
- Correctness
- Performance
- Security
`
	os.WriteFile(path, []byte(content), 0644)

	store := NewCommandStore()
	store.Discover([]string{dir})

	cmd := store.Get("review")
	if cmd == nil {
		t.Fatal("command not found")
	}
	if cmd.Name != "review" {
		t.Errorf("name=%s", cmd.Name)
	}
	if len(cmd.AllowedTools) != 3 {
		t.Errorf("tools=%v", cmd.AllowedTools)
	}
}

func TestParseAgentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reviewer.md")
	content := `---
name: code-reviewer
description: Use when reviewing PRs
model: inherit
tools: [Read, Grep, Bash]
read_only: true
---

You are a code reviewer. Focus on correctness and security.
`
	os.WriteFile(path, []byte(content), 0644)

	store := NewAgentStore()
	store.Discover([]string{dir})

	agent := store.Get("code-reviewer")
	if agent == nil {
		t.Fatal("agent not found")
	}
	if agent.Model != "inherit" {
		t.Errorf("model=%s", agent.Model)
	}
}
