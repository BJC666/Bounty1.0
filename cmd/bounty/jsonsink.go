package main

import (
	"encoding/json"
	"os"
	"sync"

	"bounty/internal/event"
)

// jsonSink serializes every agent event as one JSON object per line (JSONL).
// It backs `bounty run --json` so eval harnesses can capture structured
// transcripts (steps, usage, tool errors) without parsing console text.
type jsonSink struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func newJSONSink(w *os.File) *jsonSink {
	return &jsonSink{enc: json.NewEncoder(w)}
}

type jsonEvent struct {
	Type           string          `json:"type"`
	TextDelta      string          `json:"text_delta,omitempty"`
	ReasoningDelta string          `json:"reasoning_delta,omitempty"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolArgs       json.RawMessage `json:"tool_args,omitempty"`
	ToolResult     string          `json:"tool_result,omitempty"`
	ToolErr        string          `json:"tool_err,omitempty"`
	InputTokens    int             `json:"input_tokens,omitempty"`
	OutputTokens   int             `json:"output_tokens,omitempty"`
	CacheHit       bool            `json:"cache_hit,omitempty"`
	TurnComplete   bool            `json:"turn_complete,omitempty"`
	TurnErr        string          `json:"turn_err,omitempty"`
}

func (s *jsonSink) Emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	je := jsonEvent{Type: ev.Type}
	switch ev.Type {
	case "text":
		je.TextDelta = ev.TextDelta
	case "reasoning":
		je.ReasoningDelta = ev.ReasoningDelta
	case "notification":
		je.TextDelta = ev.TextDelta
	case "tool_call":
		je.ToolCallID = ev.ToolCallID
		je.ToolName = ev.ToolName
		je.ToolArgs = ev.ToolArgs
	case "tool_result":
		je.ToolCallID = ev.ToolCallID
		je.ToolName = ev.ToolName
		je.ToolResult = ev.ToolResult
		je.ToolErr = ev.ToolErr
	case "usage":
		if ev.Usage != nil {
			je.InputTokens = ev.Usage.InputTokens
			je.OutputTokens = ev.Usage.OutputTokens
			je.CacheHit = ev.Usage.CacheHit
		}
	case "turn_complete":
		je.TurnComplete = ev.TurnComplete
		je.TurnErr = ev.TurnErr
	}
	_ = s.enc.Encode(je)
}
