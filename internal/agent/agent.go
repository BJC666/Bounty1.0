package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bounty/internal/event"
	"bounty/internal/provider"
	"bounty/internal/tool"
)

// Runner is the interface exposed to future Coordinators and MoA (Mixture of
// Agents) implementations. A Runner accepts a user input string and runs the
// agent loop to completion.
type Runner interface {
	Run(ctx context.Context, input string) error
}

// Gate intercepts tool calls and decides whether to allow, deny, or escalate.
type Gate interface {
	Check(ctx context.Context, t tool.Tool, args json.RawMessage) (Decision, error)
}

// Decision is the verdict returned by a Gate.
type Decision int

const (
	Allow Decision = iota
	Deny
	Ask
)

// ToolHooks receives callbacks before and after every tool execution.
type ToolHooks interface {
	PreToolUse(ctx context.Context, name string, args json.RawMessage) error
	PostToolUse(ctx context.Context, name string, result string, execErr error)
}

// Asker is an optional interface for pausing the agent loop and asking the
// user a question (e.g. when a Gate returns Ask).
type Asker interface {
	Ask(ctx context.Context, question string, options []string) (string, error)
}

// Checkpointer captures file state before edits and persists turn metadata so
// the agent can rewind to a previous turn.
type Checkpointer interface {
	BeginTurn(prompt string, msgIndex int)
	Capture(path string) error
	SaveTurn(prompt string, msgIndex int) error
	GetTurn() int
}

// RepoMapProvider supplies a repository overview that changes as files
// change. Refresh returns the rendered block and whether it changed since
// the last call.
type RepoMapProvider interface {
	Refresh() (block string, changed bool)
}

// TodoSummaryProvider renders the current todo list (≤10 lines) for
// injection into the system prompt tail.
type TodoSummaryProvider interface {
	Summary() string
}

// Options configures an Agent. Zero values get sensible defaults.
type Options struct {
	MaxSteps      int
	Temperature   float64
	Sink          event.Sink
	Gate          Gate
	Hooks         ToolHooks
	Asker         Asker
	Checkpointer  Checkpointer
	MaxToolOut    int
	Insights      *SessionInsights
	Reviewer      *BackgroundReviewer
	LearningGraph *LearningGraph
	// Compact overrides compaction thresholds and the summarizer; nil = defaults.
	Compact *CompactConfig
	// MemoryDir is the project root whose .agent/memory receives background
	// review auto-memories. Empty disables auto-persistence.
	MemoryDir string
	// RepoMap, when set, refreshes the repo-map section of the system prompt
	// whenever repository files change (per user turn).
	RepoMap RepoMapProvider
	// Todos, when set, injects the current todo list into the system prompt
	// tail and refreshes it per user turn.
	Todos TodoSummaryProvider
}

// Agent is the core turn-taking loop: stream LLM output, collect tool calls,
// execute them, feed results back, repeat.
type Agent struct {
	prov         provider.Provider
	provMu       sync.RWMutex
	tools        *tool.Registry
	session      *Session
	sessMu       sync.Mutex
	maxSteps     int
	temp         float64
	sink         event.Sink
	gate         Gate
	hooks        ToolHooks
	asker        Asker
	checkpointer Checkpointer
	maxToolOut   int

	// Guardrail state
	stormSig          map[string]int
	blockedTurnStreak int

	// Last usage snapshot (atomic for safe reads from outside the loop).
	lastUsage atomic.Pointer[provider.Usage]

	// Prompt cache diagnostics.
	lastPrefixShape     provider.PrefixShape
	haveLastPrefixShape bool
	cacheStats          provider.CacheStats

	// Self-improvement
	insights   *SessionInsights
	reviewer   *BackgroundReviewer
	learnGraph *LearningGraph
	skillTurns int // turns since last skill use

	// Compaction
	compactCfg *CompactConfig

	// Project root for background-review auto-memory persistence.
	memoryDir string

	// baseSystemPrompt is the system prompt with the dynamic sections
	// stripped; refreshContext re-appends the latest blocks to it.
	baseSystemPrompt string
	repoMap          RepoMapProvider
	repoBlock        string
	todos            TodoSummaryProvider
	todoBlock        string

	// Cached-prefix tracking for accurate cache hit/miss stats (the cacheable
	// region of the previous request: all messages except the last one).
	lastCachedLen  int
	lastCachedHash string
}

// New creates an Agent. It applies defaults for zero-valued Options.
func New(prov provider.Provider, tools *tool.Registry, session *Session, opts Options) *Agent {
	if opts.MaxSteps == 0 {
		opts.MaxSteps = 50
	}
	if opts.MaxToolOut == 0 {
		opts.MaxToolOut = 32 * 1024
	}
	if opts.Sink == nil {
		opts.Sink = event.Discard
	}
	return &Agent{
		prov:             prov,
		tools:            tools,
		session:          session,
		maxSteps:         opts.MaxSteps,
		temp:             opts.Temperature,
		sink:             opts.Sink,
		gate:             opts.Gate,
		compactCfg:       opts.Compact,
		memoryDir:        opts.MemoryDir,
		baseSystemPrompt: stripRepoMap(session.SystemPrompt),
		repoMap:          opts.RepoMap,
		todos:            opts.Todos,
		hooks:            opts.Hooks,
		asker:            opts.Asker,
		checkpointer:     opts.Checkpointer,
		maxToolOut:       opts.MaxToolOut,
		stormSig:         make(map[string]int),
		insights:         opts.Insights,
		reviewer:         opts.Reviewer,
		learnGraph:       opts.LearningGraph,
	}
}

// SetSession atomically swaps the session. Guardrail counters are reset.
func (a *Agent) SetSession(s *Session) {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	a.session = s
	a.stormSig = make(map[string]int)
	a.blockedTurnStreak = 0
}

// refreshContext re-renders the dynamic system-prompt sections (repo map and
// todo list) and swaps in fresh blocks whenever they change.
func (a *Agent) refreshContext(sess *Session) {
	changed := false
	if a.repoMap != nil {
		if block, ch := a.repoMap.Refresh(); ch {
			a.repoBlock = block
			changed = true
		}
	}
	if a.todos != nil {
		if block := a.todos.Summary(); block != a.todoBlock {
			a.todoBlock = block
			changed = true
		}
	}
	if changed {
		sess.SetSystemPrompt(a.baseSystemPrompt + a.repoBlock + a.todoBlock)
	}
}

// stripRepoMap removes a trailing repo-map section (and everything after
// it) so a fresh block can be appended later.
func stripRepoMap(prompt string) string {
	if idx := strings.Index(prompt, "\n## Repo Map"); idx >= 0 {
		return prompt[:idx]
	}
	return prompt
}

// Session returns the current session.
func (a *Agent) Session() *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	return a.session
}

// SetProvider atomically swaps the LLM provider used by the agent. It is safe
// to call while a Run loop is active; the new provider takes effect on the next
// streaming call. When modelName is non-empty the session system prompt is
// rewritten so the agent can report the model it is running on.
func (a *Agent) SetProvider(p provider.Provider, modelName string) {
	a.provMu.Lock()
	a.prov = p
	a.provMu.Unlock()
	if modelName != "" {
		a.updateModelName(modelName)
	}
}

// updateModelName rewrites the model identity embedded in the session system
// prompt so the agent answers "which model are you?" truthfully after a switch.
func (a *Agent) updateModelName(model string) {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	if a.session == nil {
		return
	}
	a.session.SetSystemPrompt(rewriteModelName(a.session.SystemPrompt, model))
}

var (
	runOnRe  = regexp.MustCompile(`running on \*\*[^*]*\*\*`)
	answerRe = regexp.MustCompile(`(?m)answer with: .*$`)
)

// rewriteModelName replaces the model identity in the system prompt. It targets
// the two markers emitted by boot.buildSystemPrompt.
func rewriteModelName(prompt, model string) string {
	prompt = runOnRe.ReplaceAllString(prompt, "running on **"+model+"**")
	prompt = answerRe.ReplaceAllString(prompt, "answer with: "+model)
	return prompt
}

// provider returns the current LLM provider under a read lock.
func (a *Agent) provider() provider.Provider {
	a.provMu.RLock()
	defer a.provMu.RUnlock()
	return a.prov
}

// Run drives a single user input through the agent loop until the model stops
// requesting tool calls or the step limit is reached.
func (a *Agent) Run(ctx context.Context, input string) error {
	sess := a.Session()
	turnMsgIndex := sess.Len() // snapshot before adding user message

	if a.checkpointer != nil {
		a.checkpointer.BeginTurn(input, turnMsgIndex)
		a.checkpointer.SaveTurn(input, turnMsgIndex)
	}

	sess.Add(provider.Message{Role: "user", Content: input})

	a.refreshContext(sess)

	for step := 0; step < a.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		a.sink.Emit(event.Event{Type: "step"})

		messages := sess.Snapshot()
		schemas := a.tools.Schemas()
		prov := a.provider()

		// Compute prompt cache shape to track cache hits and misses. A hit
		// requires (a) stable system/tools/provider prefix and (b) the
		// previous cached region (all messages except the last one) still
		// being a prefix of this request — so compaction or history edits
		// correctly surface as cache misses.
		if provWithCache, ok := prov.(interface{ Version() string }); ok {
			shape := provider.ComputeShape(sess.SystemPrompt, schemas, provWithCache.Version())
			if a.haveLastPrefixShape {
				hit := true
				var miss *provider.CacheMissReason
				if r := shape.Compare(a.lastPrefixShape); r != nil {
					hit = false
					miss = r
				}
				if hit {
					currLen := len(messages) - 1
					if a.lastCachedLen > currLen ||
						provider.HashMessages(messages[:a.lastCachedLen]) != a.lastCachedHash {
						hit = false
						miss = &provider.CacheMissReason{Reasons: []string{"conversation prefix changed (compaction or history edit)"}}
					}
				}
				if hit {
					a.cacheStats.RecordHit()
				} else {
					a.cacheStats.RecordMiss(miss)
				}
			}
			a.lastPrefixShape = shape
			a.haveLastPrefixShape = true
			a.lastCachedLen = len(messages) - 1
			a.lastCachedHash = provider.HashMessages(messages[:a.lastCachedLen])
		}

		ch, err := prov.Stream(ctx, messages, schemas, provider.StreamOpts{Temperature: a.temp})
		if err != nil {
			if maxRetries, backoff, ok := retryConfig(err); ok && maxRetries > 0 {
				for retry := 0; retry < maxRetries; retry++ {
					time.Sleep(backoff(retry))
					ch, err = prov.Stream(ctx, messages, schemas, provider.StreamOpts{Temperature: a.temp})
					if err == nil {
						goto streamOK
					}
				}
			}
			return fmt.Errorf("step %d: %w", step, err)
		}
	streamOK:

		var textBuf, reasoningBuf strings.Builder
		var toolCalls []provider.ToolCall
		var usage *provider.Usage

		for ev := range ch {
			if ev.Err != nil {
				a.sink.Emit(event.Event{Type: "warning", TextDelta: fmt.Sprintf("stream error step %d: %v, recovering with partial response", step, ev.Err)})
				break
			}
			if ev.Delta != nil {
				if ev.Delta.Reasoning != "" {
					reasoningBuf.WriteString(ev.Delta.Reasoning)
					a.sink.Emit(event.Event{Type: "reasoning", ReasoningDelta: ev.Delta.Reasoning})
				}
				if ev.Delta.Content != "" {
					textBuf.WriteString(ev.Delta.Content)
					a.sink.Emit(event.Event{Type: "text", TextDelta: ev.Delta.Content})
				}
				for _, tcd := range ev.Delta.ToolCalls {
					a.mergeToolCallDelta(&toolCalls, tcd)
				}
			}
			if ev.Usage != nil {
				usage = ev.Usage
				a.lastUsage.Store(usage)
				a.sink.Emit(event.Event{Type: "usage", Usage: &event.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheHit: usage.CacheHit}})
			}
			if ev.Done {
				goto doneStreaming
			}
		}
	doneStreaming:

		// Build and record the assistant message.
		assistMsg := provider.Message{Role: "assistant", Content: textBuf.String()}
		for _, tc := range toolCalls {
			assistMsg.ToolCalls = append(assistMsg.ToolCalls, tc)
		}
		sess.Add(assistMsg)

		// No tool calls means the model produced a final answer.
		if len(toolCalls) == 0 {
			a.sink.Emit(event.Event{Type: "turn_complete", TurnComplete: true})
			return nil
		}

		// Execute tools and append results as tool messages.
		results := a.executeTools(ctx, toolCalls)
		for _, tr := range results {
			content := tr.Result
			if tr.Err != nil {
				content = "Error: " + tr.Err.Error()
			}
			sess.Add(provider.Message{Role: "tool", Content: content, ToolID: tr.CallID, ToolName: tr.Name})
		}

		a.checkGuardrails(toolCalls, results)
		if a.blockedTurnStreak >= 3 {
			return fmt.Errorf("agent stuck: %d consecutive turns with no progress", a.blockedTurnStreak)
		}

		// Record turn insights.
		if a.insights != nil {
			toolNames := make([]string, 0, len(toolCalls))
			for _, tc := range toolCalls {
				toolNames = append(toolNames, tc.Name)
			}
			tokensIn, tokensOut := 0, 0
			if lu := a.LastUsage(); lu != nil {
				tokensIn = lu.InputTokens
				tokensOut = lu.OutputTokens
			}
			a.insights.RecordTurn(tokensIn, tokensOut, toolNames, "")
			a.skillTurns++
		}

		// Update learning graph
		if a.learnGraph != nil {
			for _, tc := range toolCalls {
				a.learnGraph.Touch("tool:"+tc.Name, "tool", tc.Name)
			}
		}

		// Background review check (non-blocking).
		if a.reviewer != nil && a.reviewer.ShouldRun(step) {
			go func() {
				result := a.reviewer.RunReview(context.Background(), sess.Snapshot(), a.prov, a.memoryDir)
				if result == nil {
					return
				}
				if result.Suggestion != "" {
					a.sink.Emit(event.Event{
						Type:      "notification",
						TextDelta: "Background review: " + result.Suggestion,
					})
				}
				for _, e := range result.Saved {
					a.sink.Emit(event.Event{
						Type:      "notification",
						TextDelta: fmt.Sprintf("🧠 Auto-memory saved: %s — %s", e.Name, e.Description),
					})
				}
				for _, name := range result.Rejected {
					a.sink.Emit(event.Event{
						Type:      "notification",
						TextDelta: fmt.Sprintf("⚠️ Memory suggestion rejected (injection/self-replication markers): %s", name),
					})
				}
			}()
		}

		a.maybeCompact(ctx, sess)
	}
	return nil
}

// toolResult captures the outcome of a single tool invocation.
type toolResult struct {
	CallID string
	Name   string
	Result string
	Err    error
}

// executeTools runs a batch of tool calls. When all tools are read-only and
// there is more than one, they run in parallel.
func (a *Agent) executeTools(ctx context.Context, toolCalls []provider.ToolCall) []toolResult {
	allReadOnly := true
	for _, tc := range toolCalls {
		t, ok := a.tools.Get(tc.Name)
		if !ok || !t.ReadOnly() {
			allReadOnly = false
			break
		}
	}

	results := make([]toolResult, len(toolCalls))
	if allReadOnly && len(toolCalls) > 1 {
		var wg sync.WaitGroup
		for i, tc := range toolCalls {
			wg.Add(1)
			go func(idx int, tc provider.ToolCall) {
				defer wg.Done()
				results[idx] = a.executeOne(ctx, tc)
			}(i, tc)
		}
		wg.Wait()
	} else {
		for i, tc := range toolCalls {
			results[i] = a.executeOne(ctx, tc)
		}
	}
	return results
}

// executeOne runs a single tool call through gate, hooks, and the tool itself.
func (a *Agent) executeOne(ctx context.Context, tc provider.ToolCall) toolResult {
	t, ok := a.tools.Get(tc.Name)
	if !ok {
		return toolResult{CallID: tc.ID, Name: tc.Name, Err: fmt.Errorf("unknown tool: %s", tc.Name)}
	}

	a.sink.Emit(event.Event{Type: "tool_call", ToolCallID: tc.ID, ToolName: tc.Name, ToolArgs: tc.Args})

	if a.gate != nil {
		dec, err := a.gate.Check(ctx, t, tc.Args)
		if err != nil {
			return toolResult{CallID: tc.ID, Name: tc.Name, Err: err}
		}
		if dec == Deny {
			return toolResult{CallID: tc.ID, Name: tc.Name, Err: fmt.Errorf("%s denied by permission policy", tc.Name)}
		}
		if dec == Ask {
			if a.asker != nil {
				answer, askErr := a.asker.Ask(ctx, fmt.Sprintf("Allow %s to run %s?", tc.Name, toolLabel(tc.Name, tc.Args)), []string{"allow", "deny"})
				if askErr != nil {
					return toolResult{CallID: tc.ID, Name: tc.Name, Err: fmt.Errorf("approval query failed: %w", askErr)}
				}
				if !isApproval(answer) {
					return toolResult{CallID: tc.ID, Name: tc.Name, Err: fmt.Errorf("%s denied by user", tc.Name)}
				}
			} else {
				return toolResult{CallID: tc.ID, Name: tc.Name, Err: fmt.Errorf("%s requires user approval; retry without it or switch to a less restrictive posture", tc.Name)}
			}
		}
	}
	if a.hooks != nil {
		if err := a.hooks.PreToolUse(ctx, tc.Name, tc.Args); err != nil {
			return toolResult{CallID: tc.ID, Name: tc.Name, Err: err}
		}
	}

	// Capture pre-edit state for checkpoints (write tools only)
	if !t.ReadOnly() && a.checkpointer != nil {
		if path, ok := extractWritePath(tc.Args); ok {
			a.checkpointer.Capture(path)
		}
	}

	result, err := t.Execute(ctx, tc.Args)

	if len(result) > a.maxToolOut {
		original := len(result)
		runes := []rune(result)
		if len(runes) > a.maxToolOut/4 {
			result = string(runes[:a.maxToolOut/4])
		} else {
			// Multi-byte content within the rune budget but over the byte
			// cap — trim to the cap without splitting a UTF-8 rune.
			keep := len(result)
			for keep > a.maxToolOut {
				keep--
				for keep > 0 && (result[keep]&0xC0) == 0x80 {
					keep--
				}
			}
			result = result[:keep]
		}
		result += fmt.Sprintf("\n...[truncated: 原始输出 %d 字节，超出工具输出预算 %d 字节。请用更精确的参数缩小结果。]", original, a.maxToolOut)
	}

	if tc.Name == "todo_write" && err == nil {
		note := result
		if r := []rune(note); len(r) > 300 {
			note = string(r[:300]) + "..."
		}
		a.sink.Emit(event.Event{Type: "notification", TextDelta: "📋 " + strings.TrimSpace(note)})
	}

	if a.hooks != nil {
		a.hooks.PostToolUse(ctx, tc.Name, result, err)
	}

	tr := toolResult{CallID: tc.ID, Name: tc.Name, Result: result, Err: err}
	a.sink.Emit(event.Event{Type: "tool_result", ToolCallID: tc.ID, ToolResult: result, ToolErr: errStr(err)})
	return tr
}

// mergeToolCallDelta accumulates streaming tool-call fragments onto a running
// list. If the delta ID already exists, the ArgsDelta is appended; otherwise a
// new entry is created.
func (a *Agent) mergeToolCallDelta(toolCalls *[]provider.ToolCall, delta provider.ToolCallDelta) {
	for i := range *toolCalls {
		if (*toolCalls)[i].ID == delta.ID {
			(*toolCalls)[i].Args = append((*toolCalls)[i].Args, []byte(delta.ArgsDelta)...)
			if delta.Name != "" {
				(*toolCalls)[i].Name = delta.Name
			}
			return
		}
	}
	*toolCalls = append(*toolCalls, provider.ToolCall{ID: delta.ID, Name: delta.Name, Args: []byte(delta.ArgsDelta)})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// toolLabel returns a short, human-readable description of a tool call for
// approval prompts (the bash command or the target file path when present).
func toolLabel(name string, args json.RawMessage) string {
	var cmd struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(args, &cmd); err == nil {
		switch {
		case cmd.Command != "":
			if len(cmd.Command) > 120 {
				return cmd.Command[:120] + "..."
			}
			return cmd.Command
		case cmd.FilePath != "":
			return cmd.FilePath
		case cmd.Path != "":
			return cmd.Path
		case cmd.URL != "":
			return cmd.URL
		}
	}
	return name
}

// isApproval reports whether a user's answer to an approval prompt grants
// permission. Accepts allow/yes/y/1 (case-insensitive).
func isApproval(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "allow", "yes", "y", "1":
		return true
	}
	return false
}

// retryConfig checks whether err (or any error in its chain) carries retry
// parameters — a MaxRetries count and a BackoffFunc. It uses reflection so it
// works with RetryableError types from any provider package.
func retryConfig(err error) (maxRetries int, backoff func(int) time.Duration, ok bool) {
	for e := err; e != nil; e = errors.Unwrap(e) {
		rv := reflect.ValueOf(e)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			continue
		}
		mr := rv.FieldByName("MaxRetries")
		bf := rv.FieldByName("BackoffFunc")
		if !mr.IsValid() || !bf.IsValid() || mr.Kind() != reflect.Int {
			continue
		}
		n := int(mr.Int())
		if n <= 0 {
			continue
		}
		bfVal := bf.Interface()
		fn, isFn := bfVal.(func(int) time.Duration)
		if !isFn || fn == nil {
			continue
		}
		return n, fn, true
	}
	return 0, nil, false
}

// extractWritePath pulls the target file path from tool arguments. It handles
// both "file_path" (used by write_file, edit_file) and "path" (used by other
// tools that may write files).
func extractWritePath(args json.RawMessage) (string, bool) {
	var params struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", false
	}
	if params.FilePath != "" {
		return params.FilePath, true
	}
	if params.Path != "" {
		return params.Path, true
	}
	return "", false
}

// CacheStats returns the aggregate prompt cache performance.
func (a *Agent) CacheStats() provider.CacheStats {
	return a.cacheStats
}

// LastUsage returns the most recent provider usage snapshot.
func (a *Agent) LastUsage() *provider.Usage {
	return a.lastUsage.Load()
}
