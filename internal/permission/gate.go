package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"bounty/internal/config"
	"bounty/internal/tool"
)

// Decision is a type alias so that numeric values can be compared across
// packages without import cycles.
type Decision = int

const (
	Allow Decision = iota
	Deny
	Ask
)

// Posture describes how aggressive the gate is.
type Posture string

const (
	PostureAsk  Posture = "ask"
	PostureAuto Posture = "auto"
	PostureYolo Posture = "yolo"
	PosturePlan Posture = "plan"
)

// Gate implements a permission-checking gate based on the configuration and
// the current posture.
type Gate struct {
	cfg        config.PermissionsConfig
	sandbox    config.SandboxConfig
	posture    Posture
	allowed    map[string]bool
	forbidRead []string
}

// NewGate creates a Gate from a PermissionsConfig, the sandbox settings
// (whose ForbidRead patterns protect the read-side tools) and a Posture.
func NewGate(cfg config.PermissionsConfig, sandbox config.SandboxConfig, posture Posture) *Gate {
	g := &Gate{
		cfg:        cfg,
		sandbox:    sandbox,
		posture:    posture,
		allowed:    make(map[string]bool),
		forbidRead: sandbox.ForbidRead,
	}
	for _, t := range cfg.Allow.Tools {
		g.allowed[normalizeToolName(t)] = true
	}
	return g
}

// normalizeToolName maps human-friendly display names used in older configs
// ("Read", "WebSearch") to the registry's snake_case tool names
// ("read_file", "web_search"). Unknown names are lowercased unchanged.
func normalizeToolName(name string) string {
	switch strings.ToLower(name) {
	case "read":
		return "read_file"
	case "write":
		return "write_file"
	case "edit":
		return "edit_file"
	case "websearch":
		return "web_search"
	case "webfetch":
		return "web_fetch"
	case "todowrite":
		return "todo_write"
	case "askuserquestion":
		return "ask_user_question"
	}
	return strings.ToLower(name)
}

// Check evaluates a tool call against the permission rules and current
// posture. It returns the decision and an optional error (used on Deny).
func (g *Gate) Check(ctx context.Context, t tool.Tool, args json.RawMessage) (Decision, error) {
	if g.posture == PostureYolo {
		return Allow, nil
	}
	if g.posture == PosturePlan && !t.ReadOnly() {
		return Ask, nil
	}

	name := strings.ToLower(t.Name())

	// File read protection (ForbidRead from the sandbox config)
	for _, path := range extractReadPaths(name, args) {
		if g.isForbidRead(path) {
			return Deny, fmt.Errorf("read of %s is forbidden", path)
		}
	}

	// File write protection
	if name == "write_file" || name == "edit_file" {
		if path, ok := extractPath(args); ok {
			if g.isForbidWrite(path) {
				return Deny, fmt.Errorf("write to %s is forbidden", path)
			}
		}
	}

	// Bash pattern check
	if name == "bash" {
		cmd, ok := extractCommand(args)
		if !ok {
			return Ask, nil
		}
		for _, pattern := range g.cfg.Deny.BashPattern {
			if matchBashPattern(cmd, pattern) {
				return Deny, fmt.Errorf("command '%s' matches deny pattern '%s'", cmd, pattern)
			}
		}
		for _, pattern := range g.cfg.Allow.BashPattern {
			if matchBashPattern(cmd, pattern) {
				return Allow, nil
			}
		}
		return Ask, nil
	}

	if g.allowed[name] {
		return Allow, nil
	}
	if g.posture == PostureAsk {
		return Ask, nil
	}
	return Allow, nil
}

// isForbidWrite checks whether the given path matches any forbid-write
// pattern. The path is resolved to its absolute, symlink-resolved form first
// so that relative patterns ("Windows/*"), home-relative patterns
// ("~/.ssh/*") and symlink aliases all match their intended targets.
func (g *Gate) isForbidWrite(path string) bool {
	abs, ok := absolutePath(path)
	if !ok {
		return false
	}
	for _, pattern := range g.cfg.Deny.ForbidWrite {
		if matchesPolicy(pattern, abs) {
			return true
		}
	}
	return false
}

// isForbidRead checks whether the given path matches any forbid-read pattern
// from the sandbox configuration.
func (g *Gate) isForbidRead(path string) bool {
	abs, ok := absolutePath(path)
	if !ok {
		return false
	}
	for _, pattern := range g.forbidRead {
		if matchesPolicy(pattern, abs) {
			return true
		}
	}
	return false
}

// extractReadPaths returns the filesystem paths referenced by a read-only
// tool invocation. glob combines a directory and a pattern, so an absolute
// pattern is checked as well.
func extractReadPaths(name string, args json.RawMessage) []string {
	var params struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Pattern  string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil
	}
	var paths []string
	switch name {
	case "read_file":
		if params.FilePath != "" {
			paths = append(paths, params.FilePath)
		}
	case "grep", "code_index":
		if params.Path != "" {
			paths = append(paths, params.Path)
		}
	case "glob":
		if params.Path != "" {
			paths = append(paths, params.Path)
		}
		if filepath.IsAbs(params.Pattern) {
			paths = append(paths, params.Pattern)
		}
	}
	return paths
}

// matchBashPattern checks whether a command matches a pattern.
// A trailing " *" acts as a prefix match. Flag aliases are canonicalized
// first so deny patterns written with long forms ("git push --force *")
// also catch their short/lease variants ("git push -f", "git push
// --force-with-lease").
func matchBashPattern(cmd, pattern string) bool {
	if pattern == "*" {
		return true
	}
	cmd = normalizeBashArgs(cmd)
	if strings.HasSuffix(pattern, " *") {
		prefix := strings.TrimSuffix(pattern, " *")
		return cmd == prefix || strings.HasPrefix(cmd, prefix+" ")
	}
	return cmd == pattern
}

// normalizeBashArgs canonicalizes common short flags to their long forms
// before pattern matching.
func normalizeBashArgs(cmd string) string {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		switch {
		case f == "-f":
			fields[i] = "--force"
		case strings.HasPrefix(f, "--force"):
			fields[i] = "--force"
		}
	}
	return strings.Join(fields, " ")
}

// extractPath extracts a file path from tool arguments.
// It handles both "file_path" and "path" JSON keys.
func extractPath(args json.RawMessage) (string, bool) {
	var params struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", false
	}
	if params.FilePath != "" {
		return params.FilePath, true
	}
	if params.Path != "" {
		return params.Path, true
	}
	return "", false
}

// extractCommand extracts a command string from tool arguments.
func extractCommand(args json.RawMessage) (string, bool) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", false
	}
	cmd := strings.TrimSpace(params.Command)
	if cmd == "" {
		return "", false
	}
	return cmd, true
}
