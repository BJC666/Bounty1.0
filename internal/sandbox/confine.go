package sandbox

import (
	"os"
	"os/exec"
	"strings"
)

// Wrap restricts a command's execution environment.
// Phase 1: set working directory + strip API keys from env.
func Wrap(cmd *exec.Cmd, workspaceRoot string) *exec.Cmd {
	if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	cmd.Env = stripSecrets(base)
	return cmd
}

// stripSecrets removes model-provider credentials from an env slice.
func stripSecrets(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") &&
			!strings.HasPrefix(e, "OPENAI_API_KEY=") &&
			!strings.HasPrefix(e, "DEEPSEEK_API_KEY=") &&
			!strings.HasPrefix(e, "ANTHROPIC_AUTH_TOKEN=") {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
