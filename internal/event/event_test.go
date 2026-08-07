package event

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEventJSONRoundtrip(t *testing.T) {
	ev := Event{
		Type:      "text",
		TextDelta: "hello",
		ToolName:  "bash",
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "text" {
		t.Error("Type mismatch")
	}
	if decoded.TextDelta != "hello" {
		t.Error("TextDelta mismatch")
	}
}

func TestDiscardSink(t *testing.T) {
	Discard.Emit(Event{Type: "text"}) // should not panic
}

func TestUsageEvent(t *testing.T) {
	ev := Event{
		Type: "usage",
		Usage: &Usage{InputTokens: 100, OutputTokens: 50, CacheHit: true},
	}
	data, _ := json.Marshal(ev)
	var decoded Event
	json.Unmarshal(data, &decoded)
	if decoded.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if decoded.Usage.InputTokens != 100 {
		t.Error("InputTokens mismatch")
	}
}

type captureSink struct{ got Event }

func (c *captureSink) Emit(ev Event) { c.got = ev }

func TestFanoutRedact(t *testing.T) {
	var cap captureSink
	f := NewFanout()
	f.Redact = func(s string) string {
		return strings.ReplaceAll(s, "secret", "[redacted]")
	}
	f.Add(&cap)

	f.Emit(Event{Type: "reasoning", ReasoningDelta: "thinking about secret"})
	if cap.got.ReasoningDelta != "thinking about [redacted]" {
		t.Errorf("ReasoningDelta = %q, want redacted", cap.got.ReasoningDelta)
	}

	f.Emit(Event{Type: "text", TextDelta: "the secret is safe"})
	if cap.got.TextDelta != "the [redacted] is safe" {
		t.Errorf("TextDelta = %q, want redacted", cap.got.TextDelta)
	}

	// Structural fields must pass through untouched.
	f.Emit(Event{Type: "tool_call", ToolName: "bash", ToolArgs: json.RawMessage(`{"command":"cat secret"}`)})
	if cap.got.ToolName != "bash" || string(cap.got.ToolArgs) != `{"command":"cat secret"}` {
		t.Errorf("structural fields were modified: %+v", cap.got)
	}
}

func TestFanoutNoRedact(t *testing.T) {
	var cap captureSink
	f := NewFanout()
	f.Add(&cap)
	f.Emit(Event{Type: "text", TextDelta: "hello secret"})
	if cap.got.TextDelta != "hello secret" {
		t.Errorf("TextDelta = %q, want unchanged when Redact is nil", cap.got.TextDelta)
	}
}
