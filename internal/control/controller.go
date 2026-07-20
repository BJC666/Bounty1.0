package control

import (
	"context"
	"encoding/json"
	"sync"

	"bounty/internal/agent"
	"bounty/internal/event"
	"bounty/internal/hook"
	"bounty/internal/permission"
	"bounty/internal/provider"
	"bounty/internal/skill"
	"bounty/internal/store"
)

// Controller is the transport-agnostic session driver. One Controller sits behind
// every frontend (CLI, HTTP, WebSocket, etc.). It composes user input with
// plan mode, goals, and pending memory updates before dispatching to the agent
// runner.
type Controller struct {
	runner    agent.Runner
	sink      event.Sink
	store     *store.Store
	sessionID string
	planMode  bool
	hooks     *hook.Runner
	gate      *permission.Gate
	skills    *skill.Store
	mu        sync.Mutex
	pending   []string
	goalText  string
}

// New creates a Controller wired to the given runner, sink, store, hooks, gate,
// skills, and session identifier.
func New(runner agent.Runner, sink event.Sink, st *store.Store, hooks *hook.Runner, gate *permission.Gate, skills *skill.Store, sessionID string) *Controller {
	return &Controller{
		runner: runner, sink: sink, store: st, sessionID: sessionID,
		hooks: hooks, gate: gate, skills: skills,
	}
}

// Send composes the user input with plan mode, goal text, and any pending memory
// updates, fires the UserPromptSubmit hook, then dispatches to the agent runner.
func (c *Controller) Send(ctx context.Context, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hooks != nil {
		result, err := c.hooks.Fire(ctx, hook.UserPromptSubmit, hook.Payload{
			Event: hook.UserPromptSubmit, UserPrompt: text, SessionID: c.sessionID,
		})
		if err != nil {
			return err
		}
		if !result.Continue {
			return nil
		}
		if result.SystemMessage != "" {
			c.pending = append(c.pending, result.SystemMessage)
		}
	}

	input := c.compose(text)
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

// AgentSession returns the current agent session for persistence access.
func (c *Controller) AgentSession() *agent.Session {
	return c.runner.(*agent.Agent).Session()
}

// SaveTurn persists the current conversation state to the store.
func (c *Controller) SaveTurn() error {
	sess := c.AgentSession()
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
			title := m.Content
			if len(title) > 80 {
				title = title[:80] + "..."
			}
			return title
		}
	}
	return "New Session"
}
