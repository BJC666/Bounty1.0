package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"bounty/internal/provider"
	"bounty/internal/tool"
)

const DefaultMaxSubagentDepth = 2

// P3-4 limits: parent-context injection budget, structured-summary bounds.
const (
	maxContextSnippetsBytes = 2048 // 父任务上下文注入上限（路线图 ≤2KB）
	maxConclusionRunes      = 1200 // 结构化摘要「结论」节上限
	maxSummaryFiles         = 15   // 文件清单每类上限
)

// ── TaskTool ──

// TaskTool launches an isolated sub-agent that can read and optionally write
// files. It respects the subagent depth limit to prevent unbounded recursion.
// P3-4: role selects general (default) vs explore (read-only + structured
// report), model overrides the child provider (cheaper model), and the parent
// injects task-relevant context snippets (<=2KB) into the child.
type TaskTool struct {
	parentAgent *Agent
	maxDepth    int
}

// NewTaskTool creates a TaskTool wired to the given parent agent.
func NewTaskTool(parent *Agent, maxDepth int) *TaskTool {
	if maxDepth == 0 {
		maxDepth = DefaultMaxSubagentDepth
	}
	return &TaskTool{parentAgent: parent, maxDepth: maxDepth}
}

func (t *TaskTool) Name() string   { return "task" }
func (t *TaskTool) ReadOnly() bool { return false }

func (t *TaskTool) Description() string {
	return "Launch a sub-agent to handle a complex task. role=explore runs read-only investigation returning a structured 结论/证据/文件清单 report; role=general may write to write_paths. model optionally switches the sub-agent to another configured model. Returns a structured summary."
}

func (t *TaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"The task for the sub-agent to perform"},"role":{"type":"string","enum":["general","explore"],"description":"Sub-agent role: general (default, may write to write_paths) or explore (read-only investigation with structured report)"},"model":{"type":"string","description":"Optional model override for the sub-agent (provider/model or bare model name)"},"write_paths":{"type":"array","items":{"type":"string"},"description":"Paths the sub-agent may write to"}},"required":["task"],"additionalProperties":false}`)
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Task       string   `json:"task"`
		Role       string   `json:"role"`
		Model      string   `json:"model"`
		WritePaths []string `json:"write_paths"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	role := params.Role
	if role == "" {
		role = "general"
	}
	if role != "general" && role != "explore" {
		return "", fmt.Errorf("unknown subagent role %q (want general|explore)", role)
	}

	depth := SubagentDepth(ctx)
	if depth >= t.maxDepth {
		return "", fmt.Errorf("max subagent depth (%d) reached (current: %d)", t.maxDepth, depth)
	}

	childCtx := WithSubagentDepth(ctx, depth+1)
	return runChildAgent(childCtx, t.parentAgent, params.Task, params.WritePaths, role == "explore", role, params.Model)
}

// ── ReadOnlyTaskTool ──

// ReadOnlyTaskTool launches a read-only sub-agent for research tasks. The
// sub-agent cannot call any tool that modifies state. It is equivalent to the
// task tool with role=explore and is kept for backward compatibility.
type ReadOnlyTaskTool struct {
	parentAgent *Agent
	maxDepth    int
}

// NewReadOnlyTaskTool creates a ReadOnlyTaskTool wired to the given parent agent.
func NewReadOnlyTaskTool(parent *Agent, maxDepth int) *ReadOnlyTaskTool {
	if maxDepth == 0 {
		maxDepth = DefaultMaxSubagentDepth
	}
	return &ReadOnlyTaskTool{parentAgent: parent, maxDepth: maxDepth}
}

func (t *ReadOnlyTaskTool) Name() string   { return "read_only_task" }
func (t *ReadOnlyTaskTool) ReadOnly() bool { return true }

func (t *ReadOnlyTaskTool) Description() string {
	return "Launch a read-only sub-agent for research. Cannot modify files."
}

func (t *ReadOnlyTaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"The research task for the sub-agent"}},"required":["task"]}`)
}

func (t *ReadOnlyTaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	depth := SubagentDepth(ctx)
	if depth >= t.maxDepth {
		return "", fmt.Errorf("max subagent depth (%d) reached", t.maxDepth)
	}

	childCtx := WithSubagentDepth(ctx, depth+1)
	return runChildAgent(childCtx, t.parentAgent, params.Task, nil, true, "explore", "")
}

// ── Shared runner ──

// runChildAgent creates an isolated sub-agent, executes it, and returns a
// structured 结论/证据/文件清单 summary of the child's work.
func runChildAgent(ctx context.Context, parent *Agent, taskPrompt string, writePaths []string, readOnly bool, role, model string) (string, error) {
	childSystem := buildChildSystemPrompt(role, readOnly, writePaths)
	childSession := NewSession(childSystem)
	childSession.Add(provider.Message{Role: "user", Content: taskPrompt})

	// P3-4: inject task-relevant recent context snippets from the parent
	// session (<=2KB), so the child does not start from zero.
	if snippets := selectContextSnippets(taskPrompt, parent.Session().Snapshot(), maxContextSnippetsBytes); snippets != "" {
		childSession.Add(provider.Message{Role: "system", Content: "## 父任务上下文（与本任务相关的最近片段）\n" + snippets})
	}

	// Build filtered tool registry for the child — strip recursive delegation
	// tools, job-control tools, and (when read-only) all write tools.
	childRegistry := SubagentToolRegistry(parent.tools, readOnly)

	// Give the child half the parent's step budget, with a floor of 10.
	maxSteps := parent.maxSteps / 2
	if maxSteps < 10 {
		maxSteps = 10
	}

	childProv, err := resolveChildProvider(parent, model)
	if err != nil {
		return "", err
	}

	childAgent := New(childProv, childRegistry, childSession, Options{
		MaxSteps:    maxSteps,
		Temperature: parent.temp,
		Sink:        parent.sink,
		Gate:        parent.gate,
		MaxToolOut:  parent.maxToolOut,
	})

	if err := childAgent.Run(ctx, taskPrompt); err != nil {
		return "", fmt.Errorf("subagent failed: %w", err)
	}

	final := lastAssistantText(childSession)
	if final == "" {
		final = "Subagent completed with no output."
	}
	// P3-4: return a structured summary (结论/证据/文件清单) instead of the
	// raw final message, bounding the tokens the parent must consume.
	summary := buildSubagentSummary(final, childSession)
	// P4-1: mirror the completed sub-agent run into DeVET and auto-verify the
	// delegation chain; failures are reported as an honest note, never as a
	// hard error (verification is a defense-in-depth layer, not a gate).
	if parent.devetVerifier != nil {
		summary += parent.verifySubagentResult(ctx, role, model, final, childSession)
	}
	return summary, nil
}

// resolveChildProvider picks the child provider: the parent's provider when
// model is empty, otherwise the configured factory builds a (possibly cheaper)
// provider for the requested model.
func resolveChildProvider(parent *Agent, model string) (provider.Provider, error) {
	if model == "" {
		return parent.provider(), nil
	}
	parent.provMu.RLock()
	factory := parent.provFactory
	parent.provMu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("subagent model %q requested but no provider factory configured", model)
	}
	p, err := factory(model)
	if err != nil {
		return nil, fmt.Errorf("subagent model %q: %w", model, err)
	}
	return p, nil
}

// buildChildSystemPrompt assembles the child system prompt per role.
func buildChildSystemPrompt(role string, readOnly bool, writePaths []string) string {
	switch role {
	case "explore":
		return "You are a read-only exploration sub-agent. Investigate the assigned question using only read-only tools and return a structured report with exactly three sections: 【结论】(the answer), 【证据】(tool evidence), 【文件清单】(files examined). You cannot modify files.\n"
	default:
		p := "You are a sub-agent. Complete the assigned task and return only the final answer.\n"
		if readOnly {
			p += "You have only read-only tools. You cannot modify files.\n"
		}
		if len(writePaths) > 0 {
			p += fmt.Sprintf("You may write to: %s\n", strings.Join(writePaths, ", "))
		}
		return p
	}
}

// lastAssistantText returns the last non-empty assistant message content.
func lastAssistantText(sess *Session) string {
	msgs := sess.Snapshot()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			return msgs[i].Content
		}
	}
	return ""
}

// ── Structured summary ──

// buildSubagentSummary renders the child's work as 结论/证据/文件清单. The
// conclusion is rune-capped so the returned text is strictly bounded (and for
// verbose children, much shorter than the raw final message).
func buildSubagentSummary(final string, childSess *Session) string {
	msgs := childSess.Snapshot()

	conclusion := []rune(final)
	if len(conclusion) > maxConclusionRunes {
		conclusion = append(conclusion[:maxConclusionRunes], []rune("…（已截断）")...)
	}

	toolCounts := map[string]int{}
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolName != "" {
			toolCounts[m.ToolName]++
		}
	}
	written, read := collectChildFiles(msgs)

	var sb strings.Builder
	sb.WriteString("【结论】\n")
	sb.WriteString(string(conclusion))
	sb.WriteString("\n【证据】\n")
	if len(toolCounts) == 0 {
		sb.WriteString("- 未使用工具（直接回答）\n")
	} else {
		names := make([]string, 0, len(toolCounts))
		for n := range toolCounts {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, n := range names {
			parts = append(parts, fmt.Sprintf("%s×%d", n, toolCounts[n]))
		}
		sb.WriteString("- 工具调用：" + strings.Join(parts, "、") + "\n")
	}
	sb.WriteString("【文件清单】\n")
	if len(written) == 0 && len(read) == 0 {
		sb.WriteString("- 无\n")
	} else {
		if len(written) > 0 {
			sb.WriteString("- 修改：" + strings.Join(limitFiles(written), "、") + "\n")
		}
		if len(read) > 0 {
			sb.WriteString("- 读取：" + strings.Join(limitFiles(read), "、") + "\n")
		}
	}
	return sb.String()
}

func limitFiles(files []string) []string {
	if len(files) > maxSummaryFiles {
		return files[:maxSummaryFiles]
	}
	return files
}

// collectChildFiles extracts written/read paths from the child's tool calls.
func collectChildFiles(msgs []provider.Message) (written, read []string) {
	seenW, seenR := map[string]bool{}, map[string]bool{}
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			p := toolArgPath(tc.Args)
			if p == "" {
				continue
			}
			switch tc.Name {
			case "write_file", "edit_file":
				if !seenW[p] {
					seenW[p] = true
					written = append(written, p)
				}
			case "read_file", "glob", "grep", "code_index", "repo_map":
				if !seenR[p] {
					seenR[p] = true
					read = append(read, p)
				}
			}
		}
	}
	return written, read
}

// toolArgPath pulls a file path out of tool arguments, accepting both the
// file_path (read_file/edit_file/write_file) and path (glob/grep/code_index)
// key conventions.
func toolArgPath(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, k := range []string{"file_path", "path"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ── Parent context snippets ──

// snippetStopwords keeps high-frequency tokens (including CJK bigrams and
// single CJK particles) from creating false relevance.
var snippetStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "this": true, "that": true,
	"are": true, "was": true, "will": true, "from": true, "have": true,
}

// cjkParticles are filtered out of the CJK run before bigram extraction so
// connective characters never glue unrelated words together.
var cjkParticles = map[rune]bool{
	'的': true, '了': true, '在': true, '是': true, '和': true, '与': true,
	'我': true, '你': true, '他': true, '她': true, '把': true, '被': true,
	'给': true, '就': true, '都': true, '也': true, '很': true, '要': true,
	'请': true, '帮': true,
}

// tokenizeTask extracts relevance tokens: CJK bigrams (after particle
// filtering) and lowercase latin word tokens (>=2 chars, non-stopword).
func tokenizeTask(s string) map[string]bool {
	out := map[string]bool{}

	var cjk []rune
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			if !cjkParticles[r] {
				cjk = append(cjk, r)
			}
		}
	}
	for i := 0; i+1 < len(cjk); i++ {
		out["cjk:"+string(cjk[i:i+2])] = true
	}

	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.Is(unicode.Han, r)
	}) {
		w = strings.ToLower(w)
		if len(w) < 2 || snippetStopwords[w] {
			continue
		}
		out["w:"+w] = true
	}
	return out
}

// selectContextSnippets picks the task-relevant recent user messages from the
// parent session, highest relevance first (ties: most recent first), and packs
// them into at most maxBytes of UTF-8.
func selectContextSnippets(task string, msgs []provider.Message, maxBytes int) string {
	toks := tokenizeTask(task)
	if len(toks) == 0 || maxBytes <= 0 {
		return ""
	}

	type scored struct {
		idx     int
		content string
		score   int
	}
	var cands []scored
	for i, m := range msgs {
		if m.Role != "user" || strings.TrimSpace(m.Content) == "" {
			continue
		}
		score := 0
		for t := range tokenizeTask(m.Content) {
			if toks[t] {
				score++
			}
		}
		if score >= 2 {
			cands = append(cands, scored{idx: i, content: m.Content, score: score})
		}
	}
	if len(cands) == 0 {
		return ""
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].idx > cands[j].idx
	})

	var sb strings.Builder
	remaining := maxBytes
	for _, c := range cands {
		if remaining <= 0 {
			break
		}
		line := fmt.Sprintf("[消息%d] %s\n", c.idx, c.content)
		if len(line) > remaining {
			line = truncateBytes(line, remaining)
		}
		if line == "" {
			break
		}
		sb.WriteString(line)
		remaining -= len(line)
	}
	return strings.TrimSpace(sb.String())
}

// truncateBytes cuts s to at most max UTF-8 bytes on a rune boundary,
// reserving room for the ellipsis.
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	ellipsis := "…"
	limit := max - len(ellipsis)
	for i := limit; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return s[:i] + ellipsis
		}
	}
	return ""
}

// ── Tool registry filtering ──

// SubagentToolRegistry builds a tool registry for a sub-agent by copying
// eligible tools from the parent. Recursive delegation (task, read_only_task,
// fleet), job-control (wait, bash_output, kill_shell), and (when readOnly is
// true) all write tools are excluded.
func SubagentToolRegistry(parentRegistry *tool.Registry, readOnly bool) *tool.Registry {
	reg := tool.NewRegistry()
	for _, t := range parentRegistry.All() {
		name := t.Name()
		// Block recursive delegation.
		if name == "task" || name == "read_only_task" || name == "fleet" {
			continue
		}
		// Block job-control tools.
		if name == "wait" || name == "bash_output" || name == "kill_shell" {
			continue
		}
		if readOnly && !t.ReadOnly() {
			continue
		}
		reg.Add(t)
	}
	return reg
}
