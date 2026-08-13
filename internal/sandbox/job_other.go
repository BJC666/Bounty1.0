//go:build !windows

package sandbox

import (
	"os/exec"
)

// JobOptions configures process containment for one bash execution. On
// non-Windows platforms containment is a no-op beyond environment stripping.
type JobOptions struct {
	WorkspaceRoot string
	AllowWrite    []string
	ForbidRead    []string
	ForbidWrite   []string
	Network       bool
}

// Container is the no-op containment handle for non-Windows platforms.
type Container struct{}

func StartContained(cmd *exec.Cmd, opts JobOptions) (*Container, error) {
	cmd.Env = stripSecrets(cmd.Env)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Container{}, nil
}

func (c *Container) Close() error { return nil }
func (c *Container) Kill() error  { return nil }
