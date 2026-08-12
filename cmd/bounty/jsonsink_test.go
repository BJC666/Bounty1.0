package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"bounty/internal/event"
)

func TestJSONSinkEmitsOneLinePerEvent(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "sink-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	s := newJSONSink(f)
	s.Emit(event.Event{Type: "step"})
	s.Emit(event.Event{Type: "text", TextDelta: "hello"})
	s.Emit(event.Event{Type: "tool_call", ToolCallID: "c1", ToolName: "grep", ToolArgs: json.RawMessage(`{"pattern":"x"}`)})
	s.Emit(event.Event{Type: "tool_result", ToolCallID: "c1", ToolName: "grep", ToolResult: "hit", ToolErr: ""})
	s.Emit(event.Event{Type: "usage", Usage: &event.Usage{InputTokens: 10, OutputTokens: 2, CacheHit: true}})
	s.Emit(event.Event{Type: "turn_complete", TurnComplete: true})
	f.Close()

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, ln := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(ln) == 0 {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal(ln, &ev); err != nil {
			t.Fatalf("bad json line: %v (%s)", err, ln)
		}
		lines++
	}
	if lines != 6 {
		t.Fatalf("want 6 json lines, got %d", lines)
	}
}
