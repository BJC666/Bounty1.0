package hook

import (
	"context"
	"encoding/json"
)

type Event string

const (
	SessionStart     Event = "SessionStart"
	UserPromptSubmit Event = "UserPromptSubmit"
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	Stop             Event = "Stop"
	SubagentStop     Event = "SubagentStop"
	PreCompact       Event = "PreCompact"
	Notification     Event = "Notification"
	SessionEnd       Event = "SessionEnd"
)

type Payload struct {
	Event      Event           `json:"event"`
	SessionID  string          `json:"session_id"`
	CWD        string          `json:"cwd"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolResult string          `json:"tool_result,omitempty"`
	ToolErr    string          `json:"tool_error,omitempty"`
	UserPrompt string          `json:"user_prompt,omitempty"`
	Reason     string          `json:"reason,omitempty"`
}

type Result struct {
	Continue       bool            `json:"continue"`
	SuppressOutput bool            `json:"suppressOutput"`
	SystemMessage  string          `json:"systemMessage"`
	Decision       string          `json:"decision"`
	UpdatedInput   json.RawMessage `json:"updatedInput,omitempty"`
}

type HookConfig struct {
	Event   string
	Matcher string
	Command string
	Timeout int
}

type Runner struct {
	configs []HookConfig
}

func NewRunner(configs []HookConfig) *Runner {
	return &Runner{configs: configs}
}

func (r *Runner) Fire(ctx context.Context, event Event, payload Payload) (*Result, error) {
	for _, cfg := range r.configs {
		if string(event) != cfg.Event {
			continue
		}
		if !matchEvent(cfg.Matcher, payload) {
			continue
		}
		result, err := runShellHook(ctx, cfg, payload)
		if err != nil {
			return nil, err
		}
		if !result.Continue {
			return result, nil
		}
	}
	return &Result{Continue: true}, nil
}

func matchEvent(matcher string, payload Payload) bool {
	if matcher == "*" {
		return true
	}
	if payload.ToolName != "" && matcher == payload.ToolName {
		return true
	}
	return false
}

func (r *Runner) IsEmpty() bool { return len(r.configs) == 0 }
