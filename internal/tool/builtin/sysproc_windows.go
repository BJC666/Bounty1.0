//go:build windows

package builtin

import (
	"os/exec"
	"syscall"
)

// applyWindowsCmdLine passes the command line verbatim via SysProcAttr.CmdLine,
// bypassing os/exec's \" escaping which cmd.exe does not understand (P2-4).
func applyWindowsCmdLine(cmd *exec.Cmd, shell, shellFlag, command string) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: shell + " " + shellFlag + " " + command}
}
