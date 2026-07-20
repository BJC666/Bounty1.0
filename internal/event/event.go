package event

import "encoding/json"

// Sink is the unified event output interface consumed by all frontends.
type Sink interface {
	Emit(Event)
}

// Event is a tagged union — frontends type-switch on Type.
type Event struct {
	Type string

	// Reasoning stream
	ReasoningDelta string

	// Text stream
	TextDelta string

	// Tool calls
	ToolCallID string
	ToolName   string
	ToolArgs   json.RawMessage
	ToolResult string
	ToolErr    string

	// Usage
	Usage *Usage

	// Turn lifecycle
	TurnComplete bool
	TurnErr      error
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	CacheHit     bool
}

// Discard is the null sink for headless runs and tests.
var Discard Sink = discardSink{}

type discardSink struct{}

func (discardSink) Emit(Event) {}
