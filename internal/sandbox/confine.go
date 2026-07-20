package sandbox

import (
	"os/exec"
	"strings"
)

// Wrap restricts a command's execution environment.
// Phase 1: set working directory + strip API keys from env.
func Wrap(cmd *exec.Cmd, workspaceRoot string) *exec.Cmd {
	if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}
	env := make([]string, 0, len(cmd.Env))
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") &&
			!strings.HasPrefix(e, "OPENAI_API_KEY=") &&
			!strings.HasPrefix(e, "DEEPSEEK_API_KEY=") &&
			!strings.HasPrefix(e, "ANTHROPIC_AUTH_TOKEN=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	return cmd
}
