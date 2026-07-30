package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DockerSandbox runs commands inside an isolated Docker container.
type DockerSandbox struct {
	Image      string
	WorkDir    string
	HostDir    string // host workspace to mount
	NetworkOff bool
	ReadOnly   bool
}

// NewDockerSandbox creates a Docker sandbox with the given image and host
// workspace directory to mount inside the container.
// If image is empty, "alpine:3.21" is used as default.
func NewDockerSandbox(image, hostDir string) *DockerSandbox {
	if image == "" {
		image = "alpine:3.21"
	}
	return &DockerSandbox{
		Image:      image,
		WorkDir:    "/workspace",
		HostDir:    hostDir,
		NetworkOff: false,
		ReadOnly:   false,
	}
}

// Run executes a command inside a Docker container.
func (d *DockerSandbox) Run(ctx context.Context, command string) (string, error) {
	args := []string{"run", "--rm"}

	if d.NetworkOff {
		args = append(args, "--network=none")
	}
	if d.ReadOnly {
		args = append(args, "--read-only", "--tmpfs=/tmp")
	}

	if d.HostDir != "" {
		args = append(args, "-v", fmt.Sprintf("%s:%s", d.HostDir, d.WorkDir))
	} else {
		args = append(args, "-v", fmt.Sprintf("%s:%s", d.WorkDir, d.WorkDir))
	}
	args = append(args,
		"-w", d.WorkDir,
		d.Image,
		"sh", "-c", command,
	)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("docker: %w\n%s", err, string(output))
	}
	return string(output), nil
}

// Pull ensures the Docker image is available locally.
func (d *DockerSandbox) Pull(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", d.Image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull %s: %w\n%s", d.Image, err, string(output))
	}
	return nil
}

// Available checks if Docker is installed and running.
func Available() bool {
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

// WrapInDocker returns a function compatible with BashTool's Sandbox field
// that redirects command execution through Docker instead of running
// directly on the host.
func (d *DockerSandbox) WrapInDocker(orig string) string {
	// Escape single quotes in the original command for safe sh -c usage.
	escaped := strings.ReplaceAll(orig, "'", "'\\''")
	hostVol := d.WorkDir
	if d.HostDir != "" {
		hostVol = d.HostDir
	}
	return fmt.Sprintf("docker run --rm -w %s -v %s:%s %s sh -c '%s'",
		d.WorkDir, hostVol, d.WorkDir, d.Image, escaped)
}
