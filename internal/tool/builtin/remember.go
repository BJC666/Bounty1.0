package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"bounty/internal/memory"
	"bounty/internal/tool"
)

// RememberTool persists facts to project auto-memory.
type RememberTool struct {
	ProjectRoot string
}

func (r *RememberTool) Name() string      { return "remember" }
func (r *RememberTool) ReadOnly() bool    { return false }
func (r *RememberTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (r *RememberTool) Description() string {
	return "Save a durable fact to project memory. Use for conventions, preferences, and lessons learned."
}
func (r *RememberTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Short kebab-case name for this memory"},"description":{"type":"string","description":"One-line summary"},"content":{"type":"string","description":"The fact to remember"}},"required":["name","description","content"]}`)
}

func (r *RememberTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Injection + self-replication scan
	if !memory.IsSafeAll(params.Content) {
		return "", fmt.Errorf("memory content rejected: possible prompt injection or self-replicating prompt detected")
	}

	store := memory.NewRememberStore(r.ProjectRoot)
	if err := store.Save(params.Name, params.Description, params.Content); err != nil {
		return "", err
	}
	return fmt.Sprintf("Memory saved: %s", params.Name), nil
}
