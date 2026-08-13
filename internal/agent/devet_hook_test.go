package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/devet"
	"bounty/internal/provider"
	"bounty/internal/tool"
)

// fakeDeVETVerifier records the mirrored spec and returns a canned result.
type fakeDeVETVerifier struct {
	gotSpec *devet.MirrorSpec
	detail  devet.VerifyDetail
	err     error
}

func (f *fakeDeVETVerifier) MirrorAndVerify(ctx context.Context, spec devet.MirrorSpec) (devet.VerifyDetail, error) {
	f.gotSpec = &spec
	return f.detail, f.err
}

func newDeVETTestAgent(t *testing.T, v DeVETVerifier) *Agent {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(&fakeReadTool{name: "read_file"})
	a := New(&childSeqProvider{}, reg, NewSession("p"), Options{
		MaxSteps:      4,
		Gate:          fakeGate{dec: Allow},
		ProviderLabel: "openai",
		DeVET:         v,
	})
	return a
}

func childSessionWithTools() *Session {
	sess := NewSession("child")
	sess.Add(provider.Message{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{
		{ID: "c1", Name: "read_file", Args: json.RawMessage(`{"file_path":"a.go"}`)},
		{ID: "c2", Name: "read_file", Args: json.RawMessage(`{"file_path":"b.go"}`)},
	}})
	sess.Add(provider.Message{Role: "tool", ToolID: "c1", ToolName: "read_file", Content: "x"})
	sess.Add(provider.Message{Role: "tool", ToolID: "c2", ToolName: "read_file", Content: "y"})
	sess.Add(provider.Message{Role: "assistant", Content: "final answer"})
	return sess
}

func TestVerifySubagentResultAuthentic(t *testing.T) {
	v := &fakeDeVETVerifier{detail: devet.VerifyDetail{Authentic: true}}
	a := newDeVETTestAgent(t, v)
	section := a.verifySubagentResult(context.Background(), "explore", "deepseek-chat", "final answer", childSessionWithTools())
	if !strings.Contains(section, "【DeVET 验证】") || !strings.Contains(section, "✅") {
		t.Fatalf("section = %q", section)
	}
	if v.gotSpec == nil || v.gotSpec.HostName != "bounty-host" || v.gotSpec.HostEndpoint != "openai" {
		t.Fatalf("spec host wrong: %+v", v.gotSpec)
	}
	if len(v.gotSpec.Agents) != 1 || v.gotSpec.Agents[0].ToolCalls != 2 || v.gotSpec.Agents[0].Role != "explore" {
		t.Fatalf("spec agent wrong: %+v", v.gotSpec.Agents)
	}
	if v.gotSpec.Agents[0].ResultCommitment == "" || len(v.gotSpec.Agents[0].ResultCommitment) != 64 {
		t.Fatalf("commitment wrong: %q", v.gotSpec.Agents[0].ResultCommitment)
	}
}

func TestVerifySubagentResultDetectsForgery(t *testing.T) {
	v := &fakeDeVETVerifier{detail: devet.VerifyDetail{
		Authentic: false,
		FaultType: "subagent_proof_invalid",
		BlamePath: []string{"delegation[0]", "explore-subagent"},
		Error:     "AID mismatch",
	}}
	a := newDeVETTestAgent(t, v)
	section := a.verifySubagentResult(context.Background(), "general", "", "final answer", childSessionWithTools())
	if !strings.Contains(section, "❌") || !strings.Contains(section, "subagent_proof_invalid") {
		t.Fatalf("section = %q", section)
	}
	if !strings.Contains(section, "delegation[0] → explore-subagent") {
		t.Fatalf("blame path missing: %q", section)
	}
}

func TestVerifySubagentResultBackendDown(t *testing.T) {
	v := &fakeDeVETVerifier{err: context.DeadlineExceeded}
	a := newDeVETTestAgent(t, v)
	section := a.verifySubagentResult(context.Background(), "explore", "", "final answer", childSessionWithTools())
	if !strings.Contains(section, "⚠️ 未验证") {
		t.Fatalf("section = %q", section)
	}
}

func TestVerifySubagentResultNilVerifierDisabled(t *testing.T) {
	a := newDeVETTestAgent(t, nil)
	if got := a.verifySubagentResult(context.Background(), "explore", "", "x", childSessionWithTools()); got != "" {
		t.Fatalf("nil verifier must disable the hook, got %q", got)
	}
}

// TestRunChildAgentAutoVerifyEndToEnd proves the task tool path appends the
// DeVET section to the structured summary and passes the real tool-call count.
func TestRunChildAgentAutoVerifyEndToEnd(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&fakeWriteTool{name: "write_file"})
	reg.Add(&fakeReadTool{name: "read_file"})

	child := &childSeqProvider{final: "调研结论"}
	v := &fakeDeVETVerifier{detail: devet.VerifyDetail{Authentic: true}}
	parent := New(&childSeqProvider{}, reg, NewSession("p"), Options{
		MaxSteps: 6, Gate: fakeGate{dec: Allow}, DeVET: v, ProviderLabel: "openai",
	})
	parent.provMu.Lock()
	parent.provFactory = func(model string) (provider.Provider, error) { return child, nil }
	parent.provMu.Unlock()

	out, err := runChildAgent(context.Background(), parent, "调研一下", nil, true, "explore", "")
	if err != nil {
		t.Fatalf("runChildAgent: %v", err)
	}
	if !strings.Contains(out, "【DeVET 验证】") || !strings.Contains(out, "✅") {
		t.Fatalf("summary missing DeVET section: %q", out)
	}
	if v.gotSpec == nil {
		t.Fatal("verifier never called")
	}
	// childSeqProvider makes 2 tool calls (write_file + read_file); the child
	// records both as tool messages (the stripped write_file surfaces as a
	// missing-tool message), so the mirrored count is 2.
	if v.gotSpec.Agents[0].ToolCalls != 2 {
		t.Fatalf("tool calls = %d, want 2", v.gotSpec.Agents[0].ToolCalls)
	}
}
