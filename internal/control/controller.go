package control

import (
	"context"
	"sync"

	"bounty/internal/agent"
	"bounty/internal/event"
	"bounty/internal/hook"
	"bounty/internal/permission"
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
