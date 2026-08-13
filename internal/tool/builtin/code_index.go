package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bounty/internal/repomap"
	"bounty/internal/tool"
)

type CodeIndexTool struct{}

func (CodeIndexTool) Name() string      { return "code_index" }
func (CodeIndexTool) ReadOnly() bool    { return true }
func (CodeIndexTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (CodeIndexTool) Description() string {
	return "Index code symbols (functions, types, methods) in a file or directory. Supports Go, Python, TypeScript, Rust."
}
func (CodeIndexTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File or directory to index"},"query":{"type":"string","description":"Optional: filter symbols by name (substring match)"},"kind":{"type":"string","enum":["function","type","method","all"],"description":"Kind of symbol to return"}},"required":["path"]}`)
}

func (CodeIndexTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if params.Kind == "" {
		params.Kind = "all"
	}

	info, err := os.Stat(params.Path)
	if err != nil {
		return "", fmt.Errorf("path not found: %s", params.Path)
	}

	var files []string
	if info.IsDir() {
		filepath.Walk(params.Path, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fi.IsDir() && (fi.Name() == ".git" || fi.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !fi.IsDir() && repomap.SupportedExt(fi.Name()) {
				files = append(files, p)
			}
			return nil
		})
	} else {
		files = []string{params.Path}
	}

	if len(files) > 50 {
		files = files[:50]
	}

	var results []repomap.Symbol
	for _, f := range files {
		syms := repomap.IndexFile(f, params.Kind, 0)
		results = append(results, syms...)
	}

	// Filter by query
	if params.Query != "" {
		q := strings.ToLower(params.Query)
		filtered := make([]repomap.Symbol, 0)
		for _, r := range results {
			if strings.Contains(strings.ToLower(r.Name), q) {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// Sort by kind then name
	sort.Slice(results, func(i, j int) bool {
		if results[i].Kind != results[j].Kind {
			return results[i].Kind < results[j].Kind
		}
		return results[i].Name < results[j].Name
	})

	if len(results) > 200 {
		results = results[:200]
	}

	// Group by file for output
	var sb strings.Builder
	sb.WriteString("Code symbols:\n\n")
	currentFile := ""
	for _, r := range results {
		if r.File != currentFile {
			currentFile = r.File
			sb.WriteString(fmt.Sprintf("── %s ──\n", filepath.Base(currentFile)))
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s (line %d)\n", r.Kind, r.Name, r.Line))
	}
	if len(results) == 0 {
		sb.WriteString("No symbols found.\n")
	}
	return sb.String(), nil
}
