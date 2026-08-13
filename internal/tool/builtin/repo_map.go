package builtin

import (
	"context"
	"encoding/json"

	"bounty/internal/repomap"
	"bounty/internal/tool"
)

// RepoMapTool returns the current repository overview (file tree, symbols,
// internal dependency edges). It forces a rebuild so file changes made
// mid-task are reflected immediately.
type RepoMapTool struct {
	Manager *repomap.Manager
}

func NewRepoMapTool(m *repomap.Manager) *RepoMapTool { return &RepoMapTool{Manager: m} }

func (r *RepoMapTool) Name() string      { return "repo_map" }
func (r *RepoMapTool) ReadOnly() bool    { return true }
func (r *RepoMapTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (r *RepoMapTool) Description() string {
	return "Repository overview: layout, key symbols, dependency edges (refreshed)."
}
func (r *RepoMapTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (r *RepoMapTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if r.Manager == nil {
		return "Repo map unavailable.", nil
	}
	block := r.Manager.ForceRender()
	if block == "" {
		return "No supported source files found in the workspace.", nil
	}
	return block, nil
}
