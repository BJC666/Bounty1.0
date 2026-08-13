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
	sess := NewSession(base)

	reg := tool.NewRegistry()
	ag := New(&finisherProvider{}, reg, sess, Options{
		Gate:    allowGate{},
		Sink:    event.Discard,
		RepoMap: fake,
	})

	if err := ag.Run(context.Background(), "第一轮"); err != nil {
		t.Fatal(err)
	}
	// P8-3: the system prompt stays clean; the map is injected into the
	// first user message so the cache-stable prefix never changes.
	if got := ag.Session().SystemPrompt; got != base {
		t.Fatalf("system prompt must stay static, got %q", got)
	}
	first := firstUserMessageText(ag.Session())
	if !strings.Contains(first, "files=1") {
		t.Fatalf("first block missing from first user message: %q", first)
	}
	if !strings.HasPrefix(first, fake.block1) {
		t.Fatalf("map block must be a prefix of the first user message: %q", first)
	}

	// Flip to a changed repo, run again — the injected block must be
	// replaced, not appended; the system prompt remains byte-stable.
	fake.changed = true
	if err := ag.Run(context.Background(), "第二轮"); err != nil {
		t.Fatal(err)
	}
	first = firstUserMessageText(ag.Session())
	if !strings.Contains(first, "files=2") {
		t.Fatalf("second block missing: %q", first)
	}
	if strings.Contains(first, "files=1") {
		t.Fatalf("stale block must be replaced: %q", first)
	}
	if strings.Count(first, "## Repo Map") != 1 {
		t.Fatalf("repo map section must appear once, got %d", strings.Count(first, "## Repo Map"))
	}
	if got := ag.Session().SystemPrompt; got != base {
		t.Fatalf("system prompt changed after map refresh: %q", got)
	}
}

// firstUserMessageText returns the plain text of the first user message.
func firstUserMessageText(sess *Session) string {
	for _, m := range sess.Snapshot() {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
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

type fakeTodos struct{ text string }

func (f *fakeTodos) Summary() string { return f.text }

func TestRunInjectsTodoSummary(t *testing.T) {
	base := "You are Bounty.\n## Tool Usage Rules\n"
	repo := &fakeRepoMap{block1: "\n## Repo Map\n<!-- files=1 -->\n"}
	todos := &fakeTodos{}

	reg := tool.NewRegistry()
	ag := New(&finisherProvider{}, reg, NewSession(base), Options{
		Gate:    allowGate{},
		Sink:    event.Discard,
		RepoMap: repo,
		Todos:   todos,
	})

	todos.text = "\n## Current Todos\n- [>] 写修复\n- [ ] 跑测试\n"
	if err := ag.Run(context.Background(), "第一轮"); err != nil {
		t.Fatal(err)
	}
	first := firstUserMessageText(ag.Session())
	if !strings.Contains(first, "## Current Todos") || !strings.Contains(first, "写修复") {
		t.Fatalf("todo block missing from first user message: %q", first)
	}
	if !strings.Contains(first, "## Repo Map") {
		t.Fatalf("repo map block missing from first user message: %q", first)
	}
	if got := ag.Session().SystemPrompt; got != base {
		t.Fatalf("system prompt must stay static: %q", got)
	}

	// Todo change refreshes the injected block on the next turn.
	todos.text = "\n## Current Todos\n- [x] 写修复\n- [ ] 跑测试\n"
	if err := ag.Run(context.Background(), "第二轮"); err != nil {
		t.Fatal(err)
	}
	first = firstUserMessageText(ag.Session())
	if !strings.Contains(first, "[x] 写修复") {
		t.Fatalf("todo refresh missing: %q", first)
	}
	if strings.Count(first, "## Current Todos") != 1 {
		t.Fatalf("todo section must appear once: %q", first)
	}
	if got := ag.Session().SystemPrompt; got != base {
		t.Fatalf("system prompt changed after todo refresh: %q", got)
	}
}
