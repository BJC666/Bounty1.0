package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/event"
	"bounty/internal/provider"
	"bounty/internal/tool"
)

// fakeRepoMap returns a fresh block the first time, then the changed block
// after the test flips a flag.
type fakeRepoMap struct {
	block1  string
	block2  string
	changed bool
	calls   int
}

func (f *fakeRepoMap) Refresh() (string, bool) {
	f.calls++
	if f.changed {
		f.changed = false
		return f.block2, true
	}
	if f.calls == 1 {
		return f.block1, true
	}
	return f.block1, false
}

// finisherProvider emits one assistant text delta and Done, no tool calls.
type finisherProvider struct{}

func (f *finisherProvider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Delta: &provider.Delta{Content: "完成。"}}
	ch <- provider.StreamEvent{Done: true}
	close(ch)
	return ch, nil
}
func (f *finisherProvider) ContextWindow() int { return 100000 }

type allowGate struct{}

func (allowGate) Check(ctx context.Context, t tool.Tool, args json.RawMessage) (Decision, error) {
	return Allow, nil
}

func TestRunRefreshesRepoMapSystemPrompt(t *testing.T) {
	fake := &fakeRepoMap{
		block1: "\n## Repo Map\n<!-- files=1 -->\n- a.go\n",
		block2: "\n## Repo Map\n<!-- files=2 -->\n- a.go\n- b.go\n",
	}
	base := "You are Bounty.\n## Tool Usage Rules\n"
	sess := NewSession(base + fake.block1)

	reg := tool.NewRegistry()
	ag := New(&finisherProvider{}, reg, sess, Options{
		Gate:    allowGate{},
		Sink:    event.Discard,
		RepoMap: fake,
	})

	if err := ag.Run(context.Background(), "第一轮"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ag.Session().SystemPrompt, "files=1") {
		t.Fatalf("first block missing: %q", ag.Session().SystemPrompt)
	}
	if !strings.Contains(ag.Session().SystemPrompt, base) {
		t.Fatal("base prompt must be preserved")
	}

	// Flip to a changed repo, run again — prompt must be replaced, not appended.
	fake.changed = true
	if err := ag.Run(context.Background(), "第二轮"); err != nil {
		t.Fatal(err)
	}
	prompt := ag.Session().SystemPrompt
	if !strings.Contains(prompt, "files=2") {
		t.Fatalf("second block missing: %q", prompt)
	}
	if strings.Contains(prompt, "files=1") {
		t.Fatalf("stale block must be replaced: %q", prompt)
	}
	if strings.Count(prompt, "## Repo Map") != 1 {
		t.Fatalf("repo map section must appear once, got %d", strings.Count(prompt, "## Repo Map"))
	}
}

func TestStripRepoMap(t *testing.T) {
	base := "system\n## Repo Map\nold block"
	if got := stripRepoMap(base); got != "system" {
		t.Fatalf("got %q", got)
	}
	if got := stripRepoMap("no map here"); got != "no map here" {
		t.Fatalf("got %q", got)
	}
}
