package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, triggers, body string) string {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	data := "---\nname: " + name + "\ndescription: test skill\ntriggers: [" + triggers + "]\n---\n" + body
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	return path
}

func names(skills []*Skill) []string {
	var out []string
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

func TestMatchTriggers(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "git-workflow", "git, 提交, commit", "GIT RULES BODY")
	writeSkill(t, dir, "code-review", "review, 评审", "REVIEW RULES BODY")
	writeSkill(t, dir, "debugging", "debug, 排查", "DEBUG RULES BODY")

	st := NewStore()
	if err := st.Discover([]string{dir}); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := st.Count(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}

	// single trigger hit
	hits := st.Match("帮我看一下这个 commit 有没有问题")
	if len(hits) != 1 || hits[0].Name != "git-workflow" {
		t.Fatalf("hits = %v, want [git-workflow]", names(hits))
	}

	// case-insensitive English trigger
	hits = st.Match("Please REVIEW this PR")
	if len(hits) != 1 || hits[0].Name != "code-review" {
		t.Fatalf("hits = %v, want [code-review]", names(hits))
	}

	// multiple hits capped at maxTriggerInject, in index order
	hits = st.Match("git debug review 评审")
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2 (capped)", len(hits))
	}
	if hits[0].Name != "git-workflow" || hits[1].Name != "debugging" {
		t.Fatalf("hits = %v, want [git-workflow debugging]", names(hits))
	}

	// no hit
	if hits := st.Match("今天天气不错"); len(hits) != 0 {
		t.Fatalf("hits = %v, want none", names(hits))
	}

	// empty text
	if hits := st.Match(""); len(hits) != 0 {
		t.Fatalf("empty text hits = %v, want none", names(hits))
	}
}

func TestMatchIgnoresDisabled(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "git-workflow", "git", "GIT RULES BODY")
	writeSkill(t, dir, "debugging", "debug", "DEBUG RULES BODY")

	st := NewStore()
	if err := st.Discover([]string{dir}); err != nil {
		t.Fatalf("discover: %v", err)
	}
	st.Disable([]string{"git-workflow"})
	hits := st.Match("git status")
	if len(hits) != 0 {
		t.Fatalf("disabled skill matched: %v", names(hits))
	}
	hits = st.Match("debug 一下")
	if len(hits) != 1 || hits[0].Name != "debugging" {
		t.Fatalf("hits = %v, want [debugging]", names(hits))
	}
}
