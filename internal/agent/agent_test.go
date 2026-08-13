package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/provider"
	"bounty/internal/tool"
)

// fakeTool is a trivial tool for permission tests.
type fakeTool struct {
	name string
	ro   bool
}

func (f fakeTool) Name() string                      { return f.name }
func (f fakeTool) Description() string               { return "fake tool" }
func (f fakeTool) Schema() json.RawMessage           { return json.RawMessage(`{"type":"object"}`) }
func (f fakeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) { return "ran", nil }
func (f fakeTool) ReadOnly() bool                    { return f.ro }

// fakeGate returns a fixed decision for every tool call.
type fakeGate struct{ dec Decision }

func (g fakeGate) Check(ctx context.Context, t tool.Tool, args json.RawMessage) (Decision, error) {
	return g.dec, nil
}

// fakeAsker records questions and returns a canned answer.
type fakeAsker struct {
	answer string
	asked  bool
}

func (a *fakeAsker) Ask(ctx context.Context, question string, options []string) (string, error) {
	a.asked = true
	return a.answer, nil
}

func newTestAgent(gate Gate, asker Asker) *Agent {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "fake"})
	a := New(nil, reg, NewSession("system"), Options{
		Gate:  gate,
		Asker: asker,
	})
	return a
}

func runFake(t *testing.T, a *Agent) toolResult {
	t.Helper()
	return a.executeOne(context.Background(), providerToolCall("fake"))
}

// fakeProvider returns a canned one-message stream whose content is its model
// name, so tests can tell which provider actually served a turn.
type fakeProvider struct {
	model string
}

func (f *fakeProvider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Delta: &provider.Delta{Content: f.model}}
	ch <- provider.StreamEvent{Done: true}
	close(ch)
	return ch, nil
}

func (f *fakeProvider) ContextWindow() int { return 0 }

func TestSetProviderSwaps(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "fake"})
	sp := "You are Bounty running on **deepseek-v4-pro**. If asked which model or provider you are using, answer with: deepseek-v4-pro\n"
	a := New(&fakeProvider{model: "old"}, reg, NewSession(sp), Options{Gate: fakeGate{dec: Allow}})

	if err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	a.SetProvider(&fakeProvider{model: "new"}, "qwen3.8-max")
	if err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("second run: %v", err)
	}

	msgs := a.Session().Snapshot()
	last := msgs[len(msgs)-1]
	if last.Content != "new" {
		t.Fatalf("last assistant content = %q, want %q (provider not swapped)", last.Content, "new")
	}

	// The session system prompt (field and leading system message) must now
	// identify the new model.
	sess := a.Session()
	if !strings.Contains(sess.SystemPrompt, "answer with: qwen3.8-max") {
		t.Fatalf("SystemPrompt not rewritten: %q", sess.SystemPrompt)
	}
	if !strings.Contains(msgs[0].Content, "running on **qwen3.8-max**") {
		t.Fatalf("system message not rewritten: %q", msgs[0].Content)
	}
}

func TestRewriteModelName(t *testing.T) {
	prompt := "You are Bounty running on **deepseek-v4-pro**. If asked which model or provider you are using, answer with: deepseek-v4-pro\n"
	got := rewriteModelName(prompt, "qwen3.8-max")
	if !strings.Contains(got, "running on **qwen3.8-max**") || !strings.Contains(got, "answer with: qwen3.8-max") {
		t.Fatalf("prompt not rewritten: %q", got)
	}
	if strings.Contains(got, "deepseek-v4-pro") {
		t.Fatalf("old model still present: %q", got)
	}
}

func providerToolCall(name string) provider.ToolCall {
	return provider.ToolCall{ID: "call-1", Name: name, Args: json.RawMessage(`{}`)}
}

func TestExecuteOneDeny(t *testing.T) {
	a := newTestAgent(fakeGate{dec: Deny}, nil)
	tr := runFake(t, a)
	if tr.Err == nil || !strings.Contains(tr.Err.Error(), "denied") {
		t.Fatalf("expected denied error, got %v", tr.Err)
	}
}

func TestExecuteOneAskNoAskerRejects(t *testing.T) {
	// Without an Asker, an Ask decision must reject the tool call — it must
	// never silently run a tool that requires approval.
	a := newTestAgent(fakeGate{dec: Ask}, nil)
	tr := runFake(t, a)
	if tr.Err == nil {
		t.Fatal("expected rejection error when Ask has no Asker")
	}
	if !strings.Contains(tr.Err.Error(), "approval") {
		t.Fatalf("expected approval-required error, got %v", tr.Err)
	}
}

func TestExecuteOneAskApproved(t *testing.T) {
	asker := &fakeAsker{answer: "allow"}
	a := newTestAgent(fakeGate{dec: Ask}, asker)
	tr := runFake(t, a)
	if tr.Err != nil {
		t.Fatalf("expected approval to run tool, got %v", tr.Err)
	}
	if tr.Result != "ran" {
		t.Fatalf("expected tool result 'ran', got %q", tr.Result)
	}
	if !asker.asked {
		t.Fatal("expected Asker.Ask to be called")
	}
}

func TestExecuteOneAskRejected(t *testing.T) {
	asker := &fakeAsker{answer: "deny"}
	a := newTestAgent(fakeGate{dec: Ask}, asker)
	tr := runFake(t, a)
	if tr.Err == nil || !strings.Contains(tr.Err.Error(), "denied") {
		t.Fatalf("expected user-denied error, got %v", tr.Err)
	}
}

func TestIsApproval(t *testing.T) {
	for _, ok := range []string{"allow", "yes", "y", "Y", "1", " Allow "} {
		if !isApproval(ok) {
			t.Errorf("isApproval(%q) = false, want true", ok)
		}
	}
	for _, no := range []string{"deny", "no", "n", "0", ""} {
		if isApproval(no) {
			t.Errorf("isApproval(%q) = true, want false", no)
		}
	}
}