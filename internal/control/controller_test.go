package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bounty/internal/provider"
	"bounty/internal/skill"
)

// fakeRunner records inputs instead of talking to a model.
type fakeRunner struct {
	inputs []string
	images [][]provider.ImagePart
}

func (f *fakeRunner) Run(ctx context.Context, input string) error {
	f.inputs = append(f.inputs, input)
	return nil
}

func (f *fakeRunner) RunWithImages(ctx context.Context, input string, images []provider.ImagePart) error {
	f.inputs = append(f.inputs, input)
	f.images = append(f.images, images)
	return nil
}

func buildSkillStore(t *testing.T) *skill.Store {
	t.Helper()
	dir := t.TempDir()
	write := func(name, triggers, body string) {
		data := "---\nname: " + name + "\ndescription: d\ntriggers: [" + triggers + "]\n---\n" + body
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(data), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("git-workflow", "git, 提交", "GIT-RULES-BODY-123")
	write("code-review", "review", "REVIEW-RULES-BODY-456")
	st := skill.NewStore()
	if err := st.Discover([]string{dir}); err != nil {
		t.Fatalf("discover: %v", err)
	}
	return st
}

func TestSendInjectsMatchingSkill(t *testing.T) {
	runner := &fakeRunner{}
	ctrl := New(runner, nil, nil, nil, nil, buildSkillStore(t), nil, nil, "sess-1")

	if err := ctrl.Send(context.Background(), "帮我对这个 git 提交做检查"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	in := runner.inputs[0]
	if !strings.Contains(in, "### Skill: git-workflow") || !strings.Contains(in, "GIT-RULES-BODY-123") {
		t.Fatalf("skill body not injected:\n%s", in)
	}
	if !strings.Contains(in, "帮我对这个 git 提交做检查") {
		t.Fatalf("original text lost:\n%s", in)
	}
}

func TestSendNoSkillNoInjection(t *testing.T) {
	runner := &fakeRunner{}
	ctrl := New(runner, nil, nil, nil, nil, buildSkillStore(t), nil, nil, "sess-1")

	if err := ctrl.Send(context.Background(), "你好，今天天气不错"); err != nil {
		t.Fatalf("send: %v", err)
	}
	in := runner.inputs[0]
	if strings.Contains(in, "Activated Skills") {
		t.Fatalf("unexpected skill injection:\n%s", in)
	}
}

func TestSendWithImagesStillInjectsSkill(t *testing.T) {
	runner := &fakeRunner{}
	ctrl := New(runner, nil, nil, nil, nil, buildSkillStore(t), nil, nil, "sess-1")

	img := provider.ImagePart{MediaType: "image/png", Data: "fake"}
	if err := ctrl.SendWithImages(context.Background(), "请 review 这段代码", []provider.ImagePart{img}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(runner.inputs) != 1 || !strings.Contains(runner.inputs[0], "REVIEW-RULES-BODY-456") {
		t.Fatalf("skill not injected for image turn: %+v", runner.inputs)
	}
	if len(runner.images) != 1 || len(runner.images[0]) != 1 {
		t.Fatalf("images not forwarded: %+v", runner.images)
	}
}
