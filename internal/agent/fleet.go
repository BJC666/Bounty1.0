package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const (
	MaxFleetSize        = 64
	DefaultFleetWriters = 3
)

// FleetTool dispatches 2–64 parallel sub-agent tasks. Tasks that declare
// write_paths are serialised behind a bounded semaphore to prevent filesystem
// contention; read-only tasks run concurrently without limit.
type FleetTool struct {
	parentAgent *Agent
	maxDepth    int
	maxWriters  int
}

// NewFleetTool creates a FleetTool wired to the given parent agent.
func NewFleetTool(parent *Agent, maxDepth, maxWriters int) *FleetTool {
	if maxDepth == 0 {
		maxDepth = DefaultMaxSubagentDepth
	}
	if maxWriters == 0 {
		maxWriters = DefaultFleetWriters
	}
	return &FleetTool{parentAgent: parent, maxDepth: maxDepth, maxWriters: maxWriters}
}

func (f *FleetTool) Name() string   { return "fleet" }
func (f *FleetTool) ReadOnly() bool { return false }

func (f *FleetTool) Description() string {
	return "Run 2-64 sub-agents in parallel; writers predeclare non-overlapping write_paths."
}

func (f *FleetTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"task":{"type":"string"},"write_paths":{"type":"array","items":{"type":"string"}}}},"minItems":2,"maxItems":64}},"required":["tasks"]}`)
}

func (f *FleetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Tasks []struct {
			Task       string   `json:"task"`
			WritePaths []string `json:"write_paths"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if len(params.Tasks) < 2 {
		return "", fmt.Errorf("fleet requires at least 2 tasks")
	}
	if len(params.Tasks) > MaxFleetSize {
		return "", fmt.Errorf("max fleet size is %d", MaxFleetSize)
	}

	depth := SubagentDepth(ctx)
	if depth >= f.maxDepth {
		return "", fmt.Errorf("max subagent depth (%d) reached", f.maxDepth)
	}

	// Preflight: detect write_path conflicts across tasks.
	allPaths := make(map[string]int) // path → task index (1-based)
	for i, t := range params.Tasks {
		for _, p := range t.WritePaths {
			if prev, ok := allPaths[p]; ok {
				return "", fmt.Errorf("write_path conflict: task %d and task %d both write to %s", prev, i+1, p)
			}
			allPaths[p] = i + 1
		}
	}

	// Count writers for the aggregated summary.
	writerCount := 0
	for _, t := range params.Tasks {
		if len(t.WritePaths) > 0 {
			writerCount++
		}
	}

	// Execute tasks with bounded writer parallelism.
	sem := make(chan struct{}, f.maxWriters)
	var wg sync.WaitGroup
	results := make([]string, len(params.Tasks))

	for i, t := range params.Tasks {
		wg.Add(1)
		go func(idx int, taskPrompt string, writePaths []string) {
			defer wg.Done()
			if len(writePaths) > 0 {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			childCtx := WithSubagentDepth(ctx, depth+1)
			readOnly := len(writePaths) == 0
			out, err := runChildAgent(childCtx, f.parentAgent, taskPrompt, writePaths, readOnly, "general", "")
			if err != nil {
				results[idx] = fmt.Sprintf("Task %d ERROR: %v", idx+1, err)
			} else {
				results[idx] = fmt.Sprintf("Task %d: %s", idx+1, out)
			}
		}(i, t.Task, t.WritePaths)
	}
	wg.Wait()

	// Aggregate results.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Fleet completed %d tasks (%d writers):\n\n", len(params.Tasks), writerCount))
	for _, r := range results {
		sb.WriteString(r + "\n\n")
	}
	return sb.String(), nil
}
