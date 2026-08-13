package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"bounty/internal/provider"
	"bounty/internal/tool"
)

// recordingTool counts executions and captures the last argument payload.
type recordingTool struct {
	execs int
	last  json.RawMessage
}

func (r *recordingTool) Name() string            { return "fake" }
func (r *recordingTool) Description() string     { return "recording tool" }
func (r *recordingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (r *recordingTool) ReadOnly() bool          { return true }
func (r *recordingTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	r.execs++
	r.last = args
	return "ok", nil
}

// seqProvider emits a broken tool call on the first stream, a valid one on the
// second, and a plain answer afterwards.
type seqProvider struct{ calls int }

func (s *seqProvider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	s.calls++
	ch := make(chan provider.StreamEvent, 4)
	switch s.calls {
	case 1:
		ch <- provider.StreamEvent{Delta: &provider.Delta{ToolCalls: []provider.ToolCallDelta{{ID: "call-1", Name: "fake", ArgsDelta: `{a:x`}}}}
		ch <- provider.StreamEvent{Done: true}
	case 2:
		ch <- provider.StreamEvent{Delta: &provider.Delta{ToolCalls: []provider.ToolCallDelta{{ID: "call-1", Name: "fake", ArgsDelta: `{"file_path":"ok.txt"}`}}}}
		ch <- provider.StreamEvent{Done: true}
	default:
		ch <- provider.StreamEvent{Delta: &provider.Delta{Content: "finished"}}
		ch <- provider.StreamEvent{Done: true}
	}
	close(ch)
	return ch, nil
}

func (s *seqProvider) ContextWindow() int { return 0 }

func TestJSONRepairRetryFeedsBackOnce(t *testing.T) {
	rt := &recordingTool{}
	reg := tool.NewRegistry()
	reg.Add(rt)
	prov := &seqProvider{}
	a := New(prov, reg, NewSession("system"), Options{MaxSteps: 6, Gate: fakeGate{dec: Allow}})

	if err := a.Run(context.Background(), "please run fake"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rt.execs != 1 {
		t.Fatalf("tool executed %d times, want 1", rt.execs)
	}
	if string(rt.last) != `{"file_path":"ok.txt"}` {
		t.Fatalf("last args = %s", rt.last)
	}

	msgs := a.Session().Snapshot()
	found := false
	for _, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "不是有效 JSON") {
			found = true
		}
	}
	if !found {
		t.Fatalf("feedback tool message missing from session")
	}
	if prov.calls != 3 {
		t.Fatalf("provider called %d times, want 3 (bad -> feedback -> valid -> done)", prov.calls)
	}
}

type failingTool struct{}

func (failingTool) Name() string            { return "boom" }
func (failingTool) Description() string     { return "always fails" }
func (failingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (failingTool) ReadOnly() bool          { return true }
func (failingTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "", errors.New("no such file or directory")
}

func TestExecuteOneShapesToolError(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failingTool{})
	a := New(nil, reg, NewSession("system"), Options{Gate: fakeGate{dec: Allow}})
	tr := a.executeOne(context.Background(), provider.ToolCall{ID: "c1", Name: "boom", Args: json.RawMessage(`{}`)})
	if tr.Err == nil {
		t.Fatal("expected error")
	}
	msg := tr.Err.Error()
	if !strings.Contains(msg, "【错误类型】文件不存在") ||
		!strings.Contains(msg, "【原因】") ||
		!strings.Contains(msg, "【建议重试】") {
		t.Fatalf("error not shaped: %s", msg)
	}
}
