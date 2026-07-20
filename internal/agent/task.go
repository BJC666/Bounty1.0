package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bounty/internal/provider"
	"bounty/internal/tool"
)

const DefaultMaxSubagentDepth = 2

// ── TaskTool ──

// TaskTool launches an isolated sub-agent that can read and optionally write
// files. It respects the subagent depth limit to prevent unbounded recursion.
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
	return "Launch a sub-agent to handle a complex task. The sub-agent has its own isolated context and returns only the final answer."
}

func (t *TaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"The task for the sub-agent to perform"},"model":{"type":"string","description":"Optional model override"},"write_paths":{"type":"array","items":{"type":"string"},"description":"Paths the sub-agent may write to"}},"required":["task"]}`)
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Task       string   `json:"task"`
		Model      string   `json:"model"`
		WritePaths []string `json:"write_paths"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	depth := SubagentDepth(ctx)
	if depth >= t.maxDepth {
		return "", fmt.Errorf("max subagent depth (%d) reached (current: %d)", t.maxDepth, depth)
	}

	childCtx := WithSubagentDepth(ctx, depth+1)
	return t.runSubagent(childCtx, params.Task, params.WritePaths, false)
}

func (t *TaskTool) runSubagent(ctx context.Context, taskPrompt string, writePaths []string, readOnly bool) (string, error) {
	return runChildAgent(ctx, t.parentAgent, taskPrompt, writePaths, readOnly)
}

// ── ReadOnlyTaskTool ──

// ReadOnlyTaskTool launches a read-only sub-agent for research tasks. The
// sub-agent cannot call any tool that modifies state.
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
	return t.runSubagent(childCtx, params.Task, nil, true)
}

func (t *ReadOnlyTaskTool) runSubagent(ctx context.Context, taskPrompt string, writePaths []string, readOnly bool) (string, error) {
	return runChildAgent(ctx, t.parentAgent, taskPrompt, writePaths, readOnly)
}

// ── Shared runner ──

// runChildAgent creates an isolated sub-agent, executes it, and returns its
// final answer (the last assistant message).
func runChildAgent(ctx context.Context, parent *Agent, taskPrompt string, writePaths []string, readOnly bool) (string, error) {
	// Build isolated system prompt for the child.
	childSystem := "You are a sub-agent. Complete the assigned task and return only the final answer.\n"
	if readOnly {
		childSystem += "You have only read-only tools. You cannot modify files.\n"
	}
	if len(writePaths) > 0 {
		childSystem += fmt.Sprintf("You may write to: %s\n", strings.Join(writePaths, ", "))
	}

	childSession := NewSession(childSystem)
	childSession.Add(provider.Message{Role: "user", Content: taskPrompt})

	// Build filtered tool registry for the child — strip recursive delegation
	// tools, job-control tools, and (when read-only) all write tools.
	childRegistry := SubagentToolRegistry(parent.tools, readOnly)

	// Give the child half the parent's step budget, with a floor of 10.
	maxSteps := parent.maxSteps / 2
	if maxSteps < 10 {
		maxSteps = 10
	}

	childAgent := New(parent.prov, childRegistry, childSession, Options{
		MaxSteps:    maxSteps,
		Temperature: parent.temp,
		Sink:        parent.sink,
		Gate:        parent.gate,
		MaxToolOut:  parent.maxToolOut,
	})

	if err := childAgent.Run(ctx, taskPrompt); err != nil {
		return "", fmt.Errorf("subagent failed: %w", err)
	}

	// Extract final answer — the last assistant message with content.
	msgs := childSession.Snapshot()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			return msgs[i].Content, nil
		}
	}
	return "Subagent completed with no output.", nil
}

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
