package guardian

import (
	"context"
	"encoding/json"
	"strings"

	"bounty/internal/tool"
)

// Session is a YOLO-mode safety reviewer.
// In yolo mode, the guardian gets a chance to review sensitive operations
// before they execute.
type Session struct {
	enabled bool
}

// New creates a new guardian session.
func New(enabled bool) *Session { return &Session{enabled: enabled} }

// Review checks if a sensitive tool call should be escalated from yolo to ask.
// Returns (proceed, reason). When proceed is false, reason explains why.
func (s *Session) Review(ctx context.Context, t tool.Tool, args json.RawMessage) (bool, string) {
	if !s.enabled {
		return true, ""
	}
	name := t.Name()
	switch name {
	case "bash":
		cmd, _ := extractCommand(args)
		lower := strings.ToLower(cmd)
		if containsAny(lower, "rm -rf", "sudo", "shutdown", "reboot", "format") {
			return false, "dangerous command detected by guardian: " + cmd
		}
	case "write_file", "edit_file":
		path, _ := extractPath(args)
		if strings.Contains(path, ".env") || strings.Contains(path, ".ssh") {
			return false, "sensitive file detected by guardian: " + path
		}
	}
	return true, ""
}

// containsAny returns true if s contains any of the substrs.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
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

// extractPath extracts a file path from tool arguments.
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
