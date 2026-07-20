package hook

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

func runShellHook(ctx context.Context, cfg HookConfig, payload Payload) (*Result, error) {
	timeout := 30 * time.Second
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payloadJSON, _ := json.Marshal(payload)
	cmd := exec.CommandContext(execCtx, "sh", "-c", cfg.Command)
	cmd.Env = append(cmd.Env, "HOOK_PAYLOAD="+string(payloadJSON))

	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return &Result{Continue: false, SystemMessage: string(output)}, nil
		}
		return &Result{Continue: true, SystemMessage: string(output)}, nil
	}
	var result Result
	if json.Unmarshal(output, &result) == nil {
		return &result, nil
	}
	return &Result{Continue: true}, nil
}
