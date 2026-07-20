package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type GlobTool struct{}

func (GlobTool) Name() string        { return "glob" }
func (GlobTool) ReadOnly() bool      { return true }
func (GlobTool) Description() string { return "Find files matching a glob pattern. Returns matching file paths sorted by modification time." }
func (GlobTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"The glob pattern, e.g. **/*.go"},"path":{"type":"string","description":"Directory to search in (defaults to CWD)"}},"required":["pattern"]}`)
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
