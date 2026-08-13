package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bounty/internal/provider"
	"bounty/internal/tool"
)

// childSeqProvider emits one write_file call, one read_file call, then a long
// final text, recording every request sent to it.
type childSeqProvider struct {
	requests [][]provider.Message
	calls    int
	final    string
}

func (c *childSeqProvider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	c.requests = append(c.requests, messages)
	c.calls++
	ch := make(chan provider.StreamEvent, 4)
	switch c.calls {
	case 1:
		ch <- provider.StreamEvent{Delta: &provider.Delta{ToolCalls: []provider.ToolCallDelta{{ID: "c1", Name: "write_file", ArgsDelta: `{"file_path":"out.txt","content":"hi"}`}}}}
		ch <- provider.StreamEvent{Done: true}
	case 2:
		ch <- provider.StreamEvent{Delta: &provider.Delta{ToolCalls: []provider.ToolCallDelta{{ID: "c2", Name: "read_file", ArgsDelta: `{"file_path":"src.go"}`}}}}
		ch <- provider.StreamEvent{Done: true}
	default:
		ch <- provider.StreamEvent{Delta: &provider.Delta{Content: c.final}}
		ch <- provider.StreamEvent{Done: true}
	}
	close(ch)
	return ch, nil
}

func (c *childSeqProvider) ContextWindow() int { return 0 }

type fakeWriteTool struct{ name string }

func (f *fakeWriteTool) Name() string        { return f.name }
func (f *fakeWriteTool) ReadOnly() bool      { return false }
func (f *fakeWriteTool) Description() string { return "fake" }
func (f *fakeWriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"}},"required":["file_path","content"]}`)
}
func (f *fakeWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "written", nil
}

type fakeReadTool struct{ name string }

func (f *fakeReadTool) Name() string        { return f.name }
func (f *fakeReadTool) ReadOnly() bool      { return true }
func (f *fakeReadTool) Description() string { return "fake" }
func (f *fakeReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}`)
}
func (f *fakeReadTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "content", nil
}

// ── selectContextSnippets ──

func TestSelectContextSnippetsRelevance(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "登录接口超时怎么排查"},
		{Role: "assistant", Content: "可以加日志"},
		{Role: "user", Content: "帮我改一下首页的颜色"},
		{Role: "assistant", Content: "好的"},
		{Role: "user", Content: "登录超时和网络抖动有关吗"},
		{Role: "assistant", Content: "可能"},
	}
	got := selectContextSnippets("排查登录接口超时的问题", msgs, 2048)
	if !strings.Contains(got, "登录接口超时怎么排查") {
		t.Fatalf("snippets = %q, want the relevant login message", got)
	}
	if strings.Contains(got, "首页的颜色") {
		t.Fatalf("snippets = %q, unrelated message should be excluded", got)
	}
}

func TestSelectContextSnippetsByteCap(t *testing.T) {
	long := strings.Repeat("登录问题排查细节记录", 200)
	msgs := []provider.Message{{Role: "user", Content: long}}
	got := selectContextSnippets("登录问题", msgs, 500)
	if len(got) > 500 {
		t.Fatalf("len = %d bytes, want <= 500", len(got))
	}
	if got == "" {
		t.Fatal("relevant snippet should not be dropped entirely")
	}
}

func TestSelectContextSnippetsNoMatch(t *testing.T) {
	msgs := []provider.Message{{Role: "user", Content: "今天天气怎么样"}}
	if got := selectContextSnippets("重构数据库连接池", msgs, 2048); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// ── buildSubagentSummary ──

func TestBuildSubagentSummarySectionsAndFiles(t *testing.T) {
	sess := NewSession("child")
	sess.Add(provider.Message{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{
		{ID: "c1", Name: "write_file", Args: json.RawMessage(`{"file_path":"out.txt"}`)},
	}})
	sess.Add(provider.Message{Role: "tool", ToolID: "c1", ToolName: "write_file", Content: "written"})
	sess.Add(provider.Message{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{
		{ID: "c2", Name: "read_file", Args: json.RawMessage(`{"file_path":"src.go"}`)},
		{ID: "c3", Name: "read_file", Args: json.RawMessage(`{"file_path":"doc.md"}`)},
	}})
	sess.Add(provider.Message{Role: "tool", ToolID: "c2", ToolName: "read_file", Content: "x"})
	sess.Add(provider.Message{Role: "tool", ToolID: "c3", ToolName: "read_file", Content: "y"})
	sess.Add(provider.Message{Role: "assistant", Content: "任务完成，共 3 个 TODO"})

	summary := buildSubagentSummary("任务完成，共 3 个 TODO", sess)
	for _, want := range []string{"【结论】", "【证据】", "【文件清单】", "write_file×1", "read_file×2", "out.txt", "src.go"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestBuildSubagentSummaryShorterThanRaw(t *testing.T) {
	long := strings.Repeat("这是子代理的一长串原始输出，", 200)
	sess := NewSession("child")
	sess.Add(provider.Message{Role: "assistant", Content: long})
	summary := buildSubagentSummary(long, sess)
	if len(summary) >= len(long) {
		t.Fatalf("summary len = %d, raw len = %d, want shorter", len(summary), len(long))
	}
	if !strings.Contains(summary, "【结论】") {
		t.Fatalf("summary missing sections:\n%s", summary[:200])
	}
}

func TestBuildSubagentSummaryNoTools(t *testing.T) {
	sess := NewSession("child")
	sess.Add(provider.Message{Role: "assistant", Content: "直接回答"})
	summary := buildSubagentSummary("直接回答", sess)
	if !strings.Contains(summary, "未使用工具") {
		t.Fatalf("summary = %q", summary)
	}
}

// ── runChildAgent integration ──

func TestRunChildAgentInjectsContextAndStructuredSummary(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&fakeWriteTool{name: "write_file"})
	reg.Add(&fakeReadTool{name: "read_file"})

	parent := New(&childSeqProvider{}, reg, NewSession("parent system"), Options{
		MaxSteps: 6,
		Gate:     fakeGate{dec: Allow},
	})
	parent.Session().Add(provider.Message{Role: "user", Content: "登录接口超时怎么排查"})
	parent.Session().Add(provider.Message{Role: "assistant", Content: "可以加日志"})

	child := &childSeqProvider{final: strings.Repeat("最终答案很长。", 300)}
	factory := func(model string) (provider.Provider, error) {
		if model != "cheap/model-x" {
			t.Fatalf("factory got model %q, want cheap/model-x", model)
		}
		return child, nil
	}
	parent.provMu.Lock()
	parent.provFactory = factory
	parent.provMu.Unlock()

	taskTool := NewTaskTool(parent, DefaultMaxSubagentDepth)
	out, err := taskTool.Execute(context.Background(), json.RawMessage(`{"task":"排查登录接口超时的问题","model":"cheap/model-x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "【结论】") || !strings.Contains(out, "out.txt") {
		t.Fatalf("summary out = %q", out[:min(len(out), 300)])
	}
	if len(child.requests) == 0 {
		t.Fatal("child provider never called")
	}
	joined := ""
	for _, m := range child.requests[0] {
		joined += m.Role + ": " + m.Content + "\n"
	}
	if !strings.Contains(joined, "父任务上下文") {
		t.Fatalf("context block not injected:\n%s", joined)
	}
	if !strings.Contains(joined, "登录接口超时怎么排查") {
		t.Fatalf("relevant snippet missing:\n%s", joined)
	}
}

func TestRunChildAgentModelRequiresFactory(t *testing.T) {
	reg := tool.NewRegistry()
	parent := New(&childSeqProvider{}, reg, NewSession("p"), Options{MaxSteps: 4, Gate: fakeGate{dec: Allow}})
	taskTool := NewTaskTool(parent, DefaultMaxSubagentDepth)
	_, err := taskTool.Execute(context.Background(), json.RawMessage(`{"task":"x","model":"cheap/m"}`))
	if err == nil || !strings.Contains(err.Error(), "no provider factory") {
		t.Fatalf("err = %v, want missing factory error", err)
	}
}

func TestRunChildAgentExploreRoleReadOnlyPrompt(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&fakeWriteTool{name: "write_file"})
	reg.Add(&fakeReadTool{name: "read_file"})

	child := &childSeqProvider{final: "调研结论"}
	parent := New(&childSeqProvider{}, reg, NewSession("p"), Options{MaxSteps: 6, Gate: fakeGate{dec: Allow}})
	parent.provMu.Lock()
	parent.provFactory = func(model string) (provider.Provider, error) { return child, nil }
	parent.provMu.Unlock()

	out, err := runChildAgent(context.Background(), parent, "调研一下", nil, true, "explore", "cheap/m")
	if err != nil {
		t.Fatalf("runChildAgent: %v", err)
	}
	if !strings.Contains(out, "【结论】") {
		t.Fatalf("out = %q", out)
	}
	joined := ""
	for _, m := range child.requests[0] {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "read-only") && !strings.Contains(joined, "只读") {
		t.Fatalf("explore prompt missing read-only constraint:\n%s", joined)
	}
	if !strings.Contains(joined, "【结论】") || !strings.Contains(joined, "【文件清单】") {
		t.Fatalf("explore prompt missing report structure:\n%s", joined)
	}
}

// ── SubagentToolRegistry ──

func TestSubagentToolRegistryReadOnlyStripsWrites(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&fakeWriteTool{name: "write_file"})
	reg.Add(&fakeReadTool{name: "read_file"})
	child := SubagentToolRegistry(reg, true)
	if _, ok := child.Get("read_file"); !ok {
		t.Fatal("read tool should survive")
	}
	if _, ok := child.Get("write_file"); ok {
		t.Fatal("write tool must be stripped in read-only mode")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
