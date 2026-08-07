package control

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"bounty/internal/agent"
	"bounty/internal/event"
	"bounty/internal/hook"
	"bounty/internal/permission"
	"bounty/internal/plugin"
	"bounty/internal/provider"
	"bounty/internal/skill"
	"bounty/internal/store"
)

// Controller is the transport-agnostic session driver. One Controller sits behind
// every frontend (CLI, HTTP, WebSocket, etc.). It composes user input with
// plan mode, goals, and pending memory updates before dispatching to the agent
// runner.
type Controller struct {
	runner       agent.Runner
	sink         event.Sink
	store        *store.Store
	sessionID    string
	planMode     bool
	hooks        *hook.Runner
	gate         *permission.Gate
	skills       *skill.Store
	commands     *plugin.CommandStore
	agentDefs    *plugin.AgentStore
	mu           sync.Mutex
	pending      []string
	goalText     string
}

// New creates a Controller wired to the given runner, sink, store, hooks, gate,
// skills, commands, agents, and session identifier.
func New(runner agent.Runner, sink event.Sink, st *store.Store, hooks *hook.Runner, gate *permission.Gate, skills *skill.Store, commands *plugin.CommandStore, agents *plugin.AgentStore, sessionID string) *Controller {
	return &Controller{
		runner: runner, sink: sink, store: st, sessionID: sessionID,
		hooks: hooks, gate: gate, skills: skills,
		commands: commands, agentDefs: agents,
	}
}

// Send composes the user input with plan mode, goal text, and any pending memory
// updates, fires the UserPromptSubmit hook, then dispatches to the agent runner.
func (c *Controller) Send(ctx context.Context, text string) error {
	c.mu.Lock()
	if c.hooks != nil {
		result, err := c.hooks.Fire(ctx, hook.UserPromptSubmit, hook.Payload{
			Event: hook.UserPromptSubmit, UserPrompt: text, SessionID: c.sessionID,
		})
		if err != nil {
			c.mu.Unlock()
			return err
		}
		if !result.Continue {
			c.mu.Unlock()
			return nil
		}
		if result.SystemMessage != "" {
			c.pending = append(c.pending, result.SystemMessage)
		}
	}

	input := c.compose(text)
	c.mu.Unlock()
	return c.runner.Run(ctx, input)
}

// compose builds the final input string by layering plan mode, goal text, and
// pending memory updates on top of the user's text.
func (c *Controller) compose(text string) string {
	if c.planMode {
		text = "[Plan mode active. Use read-only tools to gather info and propose an approach.]\n\n" + text
	}
	if c.goalText != "" {
		text = "Active goal: " + c.goalText + "\n\n" + text
	}
	if len(c.pending) > 0 {
		for _, p := range c.pending {
			text = "Memory update: " + p + "\n" + text
		}
		c.pending = nil
	}
	return text
}

// SetPlanMode toggles plan mode on or off. When on, user prompts are prefixed
// with a read-only instruction so the model gathers information before acting.
func (c *Controller) SetPlanMode(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.planMode = on
}

// SetGoal sets or clears the active goal text injected into every prompt.
func (c *Controller) SetGoal(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.goalText = text
}

// AddPendingMemory enqueues a memory note to be injected into the next user
// prompt.
func (c *Controller) AddPendingMemory(note string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = append(c.pending, note)
}

// SwitchProvider hot-swaps the LLM provider used by the underlying agent. The
// new provider takes effect from the next model call; modelName is embedded in
// the session system prompt so the agent can self-identify after the switch.
func (c *Controller) SwitchProvider(p provider.Provider, modelName string) error {
	ag, ok := c.runner.(*agent.Agent)
	if !ok {
		return fmt.Errorf("runner is not an agent, cannot switch provider")
	}
	ag.SetProvider(p, modelName)
	return nil
}

// GetStore returns the underlying store for direct access (e.g., by export handlers).
func (c *Controller) GetStore() *store.Store { return c.store }

// AddSink attaches a dynamic event sink (e.g. an SSE stream). It only takes
// effect when the controller's sink is an *event.Fanout, which boot.Build
// always provides.
func (c *Controller) AddSink(s event.Sink) {
	if f, ok := c.sink.(*event.Fanout); ok {
		f.Add(s)
	}
}

// RemoveSink detaches a previously added dynamic sink.
func (c *Controller) RemoveSink(s event.Sink) {
	if f, ok := c.sink.(*event.Fanout); ok {
		f.Remove(s)
	}
}

// AgentSession returns the current agent session for persistence access.
// Returns nil if the runner is not an *agent.Agent.
func (c *Controller) AgentSession() *agent.Session {
	if ag, ok := c.runner.(*agent.Agent); ok {
		return ag.Session()
	}
	return nil
}

// SaveTurn persists the current conversation state to the store.
func (c *Controller) SaveTurn() error {
	sess := c.AgentSession()
	if sess == nil {
		return fmt.Errorf("no agent session")
	}
	messages := sess.Snapshot()

	// Update session metadata
	sessData := &store.Session{
		ID:           c.sessionID,
		Title:        extractTitle(messages),
		SystemPrompt: sess.SystemPrompt,
		Source:       "cli",
	}
	if err := c.store.SaveSession(sessData); err != nil {
		return err
	}

	// Save messages
	var storeMsgs []store.Message
	for _, m := range messages {
		toolCallsJSON := ""
		if len(m.ToolCalls) > 0 {
			b, _ := json.Marshal(m.ToolCalls)
			toolCallsJSON = string(b)
		}
		storeMsgs = append(storeMsgs, store.Message{
			SessionID: c.sessionID,
			Role:      m.Role,
			Content:   m.Content,
			ToolCalls: toolCallsJSON,
			ToolName:  m.ToolName,
		})
	}
	return c.store.SaveMessages(c.sessionID, storeMsgs)
}

// ListSessions returns recent sessions from the store.
func (c *Controller) ListSessions(limit int) ([]store.Session, error) {
	return c.store.ListSessions(limit)
}

// extractTitle extracts a human-readable title from the first user message.
func extractTitle(messages []provider.Message) string {
	for _, m := range messages {
		if m.Role == "user" && m.Content != "" {
			runes := []rune(m.Content)
			if len(runes) > 80 {
				return string(runes[:80]) + "..."
			}
			return m.Content
		}
	}
	return "New Session"
}
