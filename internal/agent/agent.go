package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

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
	Capture(path string) error
	SaveTurn(prompt string, msgIndex int) error
	GetTurn() int
}

// Options configures an Agent. Zero values get sensible defaults.
type Options struct {
	MaxSteps     int
	Temperature  float64
	Sink         event.Sink
	Gate         Gate
	Hooks        ToolHooks
	Asker        Asker
	Checkpointer Checkpointer
	MaxToolOut   int
}

// Agent is the core turn-taking loop: stream LLM output, collect tool calls,
// execute them, feed results back, repeat.
type Agent struct {
	prov         provider.Provider
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
		prov:         prov,
		tools:        tools,
		session:      session,
		maxSteps:     opts.MaxSteps,
		temp:         opts.Temperature,
		sink:         opts.Sink,
		gate:         opts.Gate,
		hooks:        opts.Hooks,
		asker:        opts.Asker,
		checkpointer: opts.Checkpointer,
		maxToolOut:   opts.MaxToolOut,
		stormSig:     make(map[string]int),
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

// Session returns the current session.
func (a *Agent) Session() *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	return a.session
}

// Run drives a single user input through the agent loop until the model stops
// requesting tool calls or the step limit is reached.
func (a *Agent) Run(ctx context.Context, input string) error {
	sess := a.Session()
	turnMsgIndex := sess.Len() // snapshot before adding user message

	if a.checkpointer != nil {
		a.checkpointer.SaveTurn(input, turnMsgIndex)
	}

	sess.Add(provider.Message{Role: "user", Content: input})

	for step := 0; step < a.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		messages := sess.Snapshot()
		schemas := a.tools.Schemas()

		ch, err := a.prov.Stream(ctx, messages, schemas, provider.StreamOpts{Temperature: a.temp})
		if err != nil {
			return fmt.Errorf("step %d: %w", step, err)
		}

		var textBuf, reasoningBuf strings.Builder
		var toolCalls []provider.ToolCall
		var usage *provider.Usage

		for ev := range ch {
			if ev.Err != nil {
				return fmt.Errorf("stream error step %d: %w", step, ev.Err)
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
		a.maybeCompact(sess)
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
			return toolResult{CallID: tc.ID, Name: tc.Name, Err: fmt.Errorf("denied")}
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
		result = result[:a.maxToolOut] + "\n... [truncated]"
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
