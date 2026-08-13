package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"bounty/internal/memory"
	"bounty/internal/tool"
)

// MemorySearchTool retrieves facts from project auto-memory. It is the read
// half of the memory loop (remember writes, memory_search reads).
type MemorySearchTool struct {
	ProjectRoot string
}

func (m *MemorySearchTool) Name() string      { return "memory_search" }
func (m *MemorySearchTool) ReadOnly() bool    { return true }
func (m *MemorySearchTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (m *MemorySearchTool) Description() string {
	return "Search project memory (facts saved by remember); empty query = recent."
}
func (m *MemorySearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Empty = recent"},"limit":{"type":"integer","description":"Max results (default 5)"}}}`)
}

type memoryHit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	CreatedAt   string `json:"created_at,omitempty"`
}

func (m *MemorySearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	var entries []memory.Entry
	var err error
	if params.Query == "" {
		entries, err = memory.Recent(m.ProjectRoot, limit)
	} else {
		entries, err = memory.Search(m.ProjectRoot, params.Query, limit)
	}
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "No matching memories found.", nil
	}

	hits := make([]memoryHit, len(entries))
	for i, e := range entries {
		hits[i] = memoryHit{
			Name:        e.Name,
			Description: e.Description,
			Content:     e.Content,
			CreatedAt:   e.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	data, err := json.MarshalIndent(hits, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Found %d memories:\n%s", len(hits), string(data)), nil
}
