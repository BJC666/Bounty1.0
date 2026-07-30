package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"bounty/internal/tool"
)

type GrepTool struct{}

func (GrepTool) Name() string   { return "grep" }
func (GrepTool) ReadOnly() bool { return true }
func (GrepTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (GrepTool) Description() string {
	return "Search file contents with regex. Prefers ripgrep (rg) when available."
}
func (GrepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"The regex pattern to search for"},"path":{"type":"string","description":"File or directory to search in"},"glob":{"type":"string","description":"Glob pattern to filter files, e.g. *.go"}},"required":["pattern"]}`)
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
	cmdArgs := []string{"-n", "--heading", "-i"}
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
		return string(output), fmt.Errorf("grep failed: %w", err)
	}
	out := string(output)
	if len(out) > 32000 {
		runes := []rune(out)
		if len(runes) > 32000/4 {
			out = string(runes[:32000/4]) + "\n... [truncated]"
		} else {
			out = out[:32000] + "\n... [truncated]"
		}
	}
	return out, nil
}
