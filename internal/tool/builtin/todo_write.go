package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bounty/internal/tool"
)

type TodoWriteTool struct{}

func (TodoWriteTool) Name() string        { return "todo_write" }
func (TodoWriteTool) ReadOnly() bool      { return true }
func (TodoWriteTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "builtin"} }
func (TodoWriteTool) Description() string { return "Create and update a task list. No host side effects — the model uses this to track progress." }
func (TodoWriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]},"activeForm":{"type":"string"}},"required":["content","status","activeForm"]}}},"required":["todos"]}`)
}
func (TodoWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
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
	var summary strings.Builder
	summary.WriteString("Todo list updated:\n")
	for _, t := range params.Todos {
		summary.WriteString(fmt.Sprintf("  [%s] %s\n", t.Status, t.Content))
	}
	return summary.String(), nil
}
