package builtin

import (
	"context"
	"encoding/json"
	"fmt"
)

type WebSearchTool struct{}

func (WebSearchTool) Name() string   { return "web_search" }
func (WebSearchTool) ReadOnly() bool { return true }
func (WebSearchTool) Description() string {
	return "Search the web. Returns result blocks with titles and URLs."
}
func (WebSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"The search query"},"allowed_domains":{"type":"array","items":{"type":"string"}},"blocked_domains":{"type":"array","items":{"type":"string"}}},"required":["query"]}`)
}
func (WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query          string   `json:"query"`
		AllowedDomains []string `json:"allowed_domains"`
		BlockedDomains []string `json:"blocked_domains"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	// Phase 1: placeholder — real implementation needs a search API backend
	return fmt.Sprintf("Web search for: %s\n(Search backend not yet configured. Add a search API key to enable.)", params.Query), nil
}
