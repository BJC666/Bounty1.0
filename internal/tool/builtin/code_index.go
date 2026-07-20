package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"bounty/internal/tool"
)

type CodeIndexTool struct{}

func (CodeIndexTool) Name() string   { return "code_index" }
func (CodeIndexTool) ReadOnly() bool { return true }
func (CodeIndexTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (CodeIndexTool) Description() string {
	return "Index code symbols (functions, types, methods) in a file or directory. Supports Go, Python, TypeScript, Rust."
}
func (CodeIndexTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File or directory to index"},"query":{"type":"string","description":"Optional: filter symbols by name (substring match)"},"kind":{"type":"string","enum":["function","type","method","all"],"description":"Kind of symbol to return"}},"required":["path"]}`)
}

// symbolPatterns maps language → kind → regex
var symbolPatterns = map[string]map[string]*regexp.Regexp{
	".go": {
		"function": regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(`),
		"type":     regexp.MustCompile(`^type\s+(\w+)\s+(?:struct|interface|int|string|float|bool|map|chan|\[)`),
		"method":   regexp.MustCompile(`^func\s+\([^)]*\*?\w+\)\s+(\w+)\s*\(`),
	},
	".py": {
		"function": regexp.MustCompile(`^def\s+(\w+)\s*\(`),
		"type":     regexp.MustCompile(`^class\s+(\w+)\s*[:(]`),
		"method":   regexp.MustCompile(`^\s+def\s+(\w+)\s*\(self`),
	},
	".ts": {
		"function": regexp.MustCompile(`(?:function|async function)\s+(\w+)\s*\(`),
		"type":     regexp.MustCompile(`(?:interface|type|class)\s+(\w+)`),
		"method":   regexp.MustCompile(`(?:public|private|protected)?\s*(?:async\s+)?(\w+)\s*\([^)]*\)\s*:`),
	},
	".tsx": {
		"function": regexp.MustCompile(`(?:function|const)\s+(\w+)`),
		"type":     regexp.MustCompile(`(?:interface|type|class)\s+(\w+)`),
	},
	".js": {
		"function": regexp.MustCompile(`(?:function|async function)\s+(\w+)\s*\(`),
		"type":     regexp.MustCompile(`(?:class)\s+(\w+)`),
	},
	".rs": {
		"function": regexp.MustCompile(`^(\s*)fn\s+(\w+)\s*[<(]`),
		"type":     regexp.MustCompile(`^(?:\s*)pub?\s*(?:struct|enum|trait|type)\s+(\w+)`),
	},
}

type symbolResult struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
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
			if !fi.IsDir() {
				ext := filepath.Ext(fi.Name())
				if _, ok := symbolPatterns[ext]; ok {
					files = append(files, p)
				}
			}
			return nil
		})
	} else {
		files = []string{params.Path}
	}

	if len(files) > 50 {
		files = files[:50]
	}

	var results []symbolResult
	for _, f := range files {
		syms, _ := indexFile(f, params.Kind)
		results = append(results, syms...)
	}

	// Filter by query
	if params.Query != "" {
		q := strings.ToLower(params.Query)
		filtered := make([]symbolResult, 0)
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

func indexFile(path string, kindFilter string) ([]symbolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	ext := filepath.Ext(path)
	patterns, ok := symbolPatterns[ext]
	if !ok {
		return nil, nil
	}

	var results []symbolResult
	for i, line := range lines {
		for kind, re := range patterns {
			if kindFilter != "all" && kindFilter != kind {
				continue
			}
			matches := re.FindStringSubmatch(line)
			if matches == nil {
				continue
			}
			name := matches[len(matches)-1] // last capture group
			if name == "" {
				continue
			}
			results = append(results, symbolResult{
				Name: name, Kind: kind, File: path, Line: i + 1,
			})
		}
	}
	return results, nil
}
