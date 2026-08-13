package event

import (
	"encoding/json"
	"sync"
)

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
	Usage *Usage `json:"Usage,omitempty"`

	// Turn lifecycle
	TurnComplete bool   `json:"TurnComplete,omitempty"`
	TurnErr      string `json:"TurnErr,omitempty"`

	// DeVET sub-agent verification (P4-1): emitted after a task/fleet
	// sub-agent result has been mirrored and verified.
	Devet *DeVETEvent `json:"Devet,omitempty"`
}

// DeVETEvent carries one sub-agent verification outcome for live frontends.
type DeVETEvent struct {
	HostName  string            `json:"host_name"`
	Agents    []DeVETAgentEvent `json:"agents"`
	Authentic bool              `json:"authentic"`
	Fault     string            `json:"fault,omitempty"`
	Blame     []string          `json:"blame,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// DeVETAgentEvent is one delegate row in the chain panel.
type DeVETAgentEvent struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Model        string   `json:"model"`
	Commitment   string   `json:"commitment"`
	ToolCalls    int      `json:"tool_calls"`
	WrittenFiles []string `json:"written_files,omitempty"`
	FaultType    string   `json:"fault_type,omitempty"`
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

// Fanout is a Sink that broadcasts every event to all registered sinks.
// Build uses one fanout so the agent, controller, and dynamically attached
// frontends (e.g. an SSE stream) all observe the same events.
type Fanout struct {
	mu    sync.Mutex
	sinks []Sink
	// Redact, when set, is applied to TextDelta and ReasoningDelta before
	// events reach any sink. It lets callers strip secrets (API keys,
	// private keys) from live streams without touching persisted data.
	Redact func(string) string
}

func NewFanout() *Fanout { return &Fanout{} }

// Add registers a sink to receive future events.
func (f *Fanout) Add(s Sink) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinks = append(f.sinks, s)
}

// Remove unregisters a sink.
func (f *Fanout) Remove(s Sink) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.sinks {
		if existing == s {
			f.sinks = append(f.sinks[:i], f.sinks[i+1:]...)
			return
		}
	}
}

func (f *Fanout) Emit(ev Event) {
	f.mu.Lock()
	sinks := append([]Sink(nil), f.sinks...)
	redact := f.Redact
	f.mu.Unlock()
	if redact != nil {
		ev.TextDelta = redact(ev.TextDelta)
		ev.ReasoningDelta = redact(ev.ReasoningDelta)
	}
	for _, s := range sinks {
		s.Emit(ev)
	}
}
