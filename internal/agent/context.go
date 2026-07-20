package agent

import "context"

type subagentDepthKey struct{}

// WithSubagentDepth stores the current subagent nesting depth in ctx.
// Negative values are clamped to 0.
func WithSubagentDepth(ctx context.Context, depth int) context.Context {
	if depth < 0 {
		depth = 0
	}
	return context.WithValue(ctx, subagentDepthKey{}, depth)
}

// SubagentDepth returns the current subagent nesting depth (0 for the
// top-level agent).
func SubagentDepth(ctx context.Context) int {
	if d, ok := ctx.Value(subagentDepthKey{}).(int); ok {
		return d
	}
	return 0
}
