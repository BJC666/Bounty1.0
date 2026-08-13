package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bounty/internal/tool"
)

type GlobTool struct{}

func (GlobTool) Name() string        { return "glob" }
func (GlobTool) ReadOnly() bool      { return true }
func (GlobTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "builtin"} }
func (GlobTool) Description() string { return "Find files by glob pattern (*, ?, [...]). Returns paths sorted by mtime." }
func (GlobTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","maxLength":512,"description":"Pattern, e.g. *.go"},"path":{"type":"string","maxLength":1024,"description":"Dir (default CWD)"}},"required":["pattern"]}`)
}
func (GlobTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	base := params.Path
	if base == "" {
		base, _ = os.Getwd()
	}
	pattern := filepath.Join(base, params.Pattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	if len(matches) > 100 {
		matches = matches[:100]
	}
	return strings.Join(matches, "\n"), nil
}
