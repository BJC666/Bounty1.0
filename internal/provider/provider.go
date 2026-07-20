package provider

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"name,omitempty"`
	ToolID    string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"arguments"`
}

type Delta struct {
	Role      string
	Content   string
	Reasoning string
	ToolCalls []ToolCallDelta
}

type ToolCallDelta struct {
	ID        string
	Name      string
	ArgsDelta string
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	CacheHit     bool
}

type StreamEvent struct {
	Delta *Delta
	Usage *Usage
	Done  bool
	Err   error
}

type StreamOpts struct {
	Temperature float64
	MaxTokens   int
	Effort      string
}

type Provider interface {
	Stream(ctx context.Context, messages []Message, tools []json.RawMessage, opts StreamOpts) (<-chan StreamEvent, error)
}
