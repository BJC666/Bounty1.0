package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"bounty/internal/tool"
)

type GrepTool struct{}

func (GrepTool) Name() string      { return "grep" }
func (GrepTool) ReadOnly() bool    { return true }
func (GrepTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (GrepTool) Description() string {
	return "Search file contents with regex. Prefers ripgrep (rg) when available."
}
func (GrepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","maxLength":1000,"description":"The regex pattern to search for"},"path":{"type":"string","maxLength":1024,"description":"File or directory to search in"},"glob":{"type":"string","maxLength":512,"description":"Glob pattern to filter files, e.g. *.go"}},"required":["pattern"],"additionalProperties":false}`)
}
func (GrepTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	cmdArgs := []string{"-n", "-i"}
	if params.Glob != "" {
		cmdArgs = append(cmdArgs, "-g", params.Glob)
	}
	cmdArgs = append(cmdArgs, params.Pattern)
	if params.Path != "" {
		cmdArgs = append(cmdArgs, params.Path)
	}
	cmd := exec.CommandContext(ctx, "rg", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found.", nil
		}
		// If rg not found, fall back to Go native regex
		if _, ok := err.(*exec.Error); ok {
			return grepFallback(params)
		}
		return string(output), fmt.Errorf("grep failed: %w", err)
	}
	out := capMatchLines(string(output))
	return out, nil
}

// grepMaxMatches caps grep output at the first 300 matches and tells the
// model how to narrow the search.
const grepMaxMatches = 300

// capMatchLines keeps the first grepMaxMatches lines (each line is one match
// because rg runs with -n and without --heading) and appends a narrowing
// hint with the true total.
func capMatchLines(out string) string {
	trimmed := strings.TrimRight(out, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= grepMaxMatches {
		return out
	}
	shown := strings.Join(lines[:grepMaxMatches], "\n")
	return shown + fmt.Sprintf("\n...[grep truncated: 共 %d 条匹配，仅显示前 %d 条。请收窄 pattern、glob 或指定 path 缩小范围后重试。]", len(lines), grepMaxMatches)
}

// grepFallback searches files using Go's regexp when ripgrep is not available.
func grepFallback(params struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
}) (string, error) {
	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	var results []string
	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if params.Glob != "" {
			matched, _ := filepath.Match(params.Glob, info.Name())
			if !matched {
				return nil
			}
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", p, i+1, line))
			}
		}
		return nil
	})

	if len(results) == 0 {
		return "No matches found.", nil
	}
	if len(results) > grepMaxMatches {
		shown := strings.Join(results[:grepMaxMatches], "\n")
		return shown + fmt.Sprintf("\n...[grep truncated: 共 %d 条匹配，仅显示前 %d 条。请收窄 pattern、glob 或指定 path 缩小范围后重试。]", len(results), grepMaxMatches), nil
	}
	out := strings.Join(results, "\n")
	if len(out) > 32000 {
		out = out[:32000] + "\n... [truncated]"
	}
	return out, nil
}
