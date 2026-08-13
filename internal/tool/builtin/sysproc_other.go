//go:build !windows

package builtin

import "os/exec"

// applyWindowsCmdLine is a no-op on non-Windows platforms.
func applyWindowsCmdLine(cmd *exec.Cmd, shell, shellFlag, command string) {}
