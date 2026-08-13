package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bounty/internal/store"
	"bounty/internal/tool"
)

// TodoBackend persists todo lists per session. store.Store implements it;
// tests use a fake.
type TodoBackend interface {
	ReplaceTodos(sessionID string, todos []store.Todo) error
	LoadTodos(sessionID string) ([]store.Todo, error)
}

// TodoWriteTool persists the model's task list to SQLite so the host can
// display it and future turns can inject it back into the system prompt.
type TodoWriteTool struct {
	Store     TodoBackend
	SessionID string
}

func (t *TodoWriteTool) Name() string      { return "todo_write" }
func (t *TodoWriteTool) ReadOnly() bool    { return true }
func (t *TodoWriteTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (t *TodoWriteTool) Description() string {
	return "Create and update the task list. The list is persisted per session, shown in the UI, and re-injected into context each turn."
}
func (t *TodoWriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]},"activeForm":{"type":"string"}},"required":["content","status","activeForm"]}}},"required":["todos"]}`)
}

func (t *TodoWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Todos []struct {
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
		} `json:"todos"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Normalize + validate
	items := make([]store.Todo, 0, len(params.Todos))
	var done, inProgress int
	for _, raw := range params.Todos {
		status := raw.Status
		switch status {
		case "pending", "in_progress", "completed":
		default:
			status = "pending"
		}
		content := strings.TrimSpace(raw.Content)
		if content == "" {
			continue
		}
		items = append(items, store.Todo{Content: content, Status: status, ActiveForm: raw.ActiveForm})
		switch status {
		case "completed":
			done++
		case "in_progress":
			inProgress++
		}
	}

	if t.Store != nil {
		if err := t.Store.ReplaceTodos(t.SessionID, items); err != nil {
			return "", err
		}
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Todo list updated (%d 项：%d 已完成，%d 进行中):\n", len(items), done, inProgress))
	for _, it := range items {
		summary.WriteString(fmt.Sprintf("  [%s] %s\n", it.Status, it.Content))
	}
	if t.Store == nil {
		summary.WriteString("(未连接持久层，仅回显)\n")
	}
	return summary.String(), nil
}
