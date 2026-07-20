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
	cfg     config.PermissionsConfig
	posture Posture
	allowed map[string]bool
}

// NewGate creates a Gate from a PermissionsConfig and Posture.
func NewGate(cfg config.PermissionsConfig, posture Posture) *Gate {
	g := &Gate{cfg: cfg, posture: posture, allowed: make(map[string]bool)}
	for _, t := range cfg.Allow.Tools {
		g.allowed[strings.ToLower(t)] = true
	}
	return g
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
// pattern (both relative and absolute).
func (g *Gate) isForbidWrite(path string) bool {
	for _, pattern := range g.cfg.Deny.ForbidWrite {
		matched, _ := filepath.Match(pattern, path)
		if matched {
			return true
		}
		abs, _ := filepath.Abs(path)
		if matched, _ := filepath.Match(pattern, abs); matched {
			return true
		}
	}
	return false
}

// matchBashPattern checks whether a command matches a pattern.
// A trailing " *" acts as a prefix match.
func matchBashPattern(cmd, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, " *") {
		prefix := strings.TrimSuffix(pattern, " *")
		return cmd == prefix || strings.HasPrefix(cmd, prefix+" ")
	}
	return cmd == pattern
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
