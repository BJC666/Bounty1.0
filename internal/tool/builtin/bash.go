package builtin

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"bounty/internal/tool"
)

type BashTool struct {
	Timeout      time.Duration
	Sandbox      func(*exec.Cmd) *exec.Cmd
	DockerRunner func(ctx context.Context, command string) (string, error)
}

func (b *BashTool) Name() string   { return "bash" }
func (b *BashTool) ReadOnly() bool { return false }
func (b *BashTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (b *BashTool) Description() string {
	return "Execute a shell command. Use for running tests, building, file operations, git commands, and other terminal tasks."
}
func (b *BashTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The shell command to execute"},"description":{"type":"string","description":"Clear, concise description of what this command does"},"timeout":{"type":"number","description":"Optional timeout in milliseconds (max 600000)"}},"required":["command","description"]}`)
}
func (b *BashTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Command     string  `json:"command"`
		Description string  `json:"description"`
		Timeout     float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	timeout := b.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Millisecond
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// If Docker runner is configured, use it for isolated execution.
	if b.DockerRunner != nil {
		output, err := b.DockerRunner(execCtx, params.Command)
		if err != nil {
			return output, &ExecError{Command: params.Command, Output: output, Err: err}
		}
		return output, nil
	}

	cmd := exec.CommandContext(execCtx, "sh", "-c", params.Command)
	if b.Sandbox != nil {
		cmd = b.Sandbox(cmd)
	}
	output, err := cmd.CombinedOutput()
	if execCtx.Err() == context.DeadlineExceeded {
		return "", &TimeoutError{Command: params.Command, Timeout: timeout}
	}
	if err != nil {
		return string(output), &ExecError{Command: params.Command, Output: string(output), Err: err}
	}
	return string(output), nil
}

type TimeoutError struct {
	Command string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string { return "command timed out after " + e.Timeout.String() + ": " + e.Command }

type ExecError struct {
	Command string
	Output  string
	Err     error
}

func (e *ExecError) Error() string { return e.Output + "\n" + e.Err.Error() }
