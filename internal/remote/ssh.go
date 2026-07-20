package remote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SSHSession executes commands on a remote host via SSH.
type SSHSession struct {
	Host string
	User string
	Key  string // path to SSH key
	Port int
}

func NewSSH(host, user, key string, port int) *SSHSession {
	if port == 0 {
		port = 22
	}
	return &SSHSession{Host: host, User: user, Key: key, Port: port}
}

// Run executes a command on the remote host via ssh.
func (s *SSHSession) Run(ctx context.Context, command string) (string, error) {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-p", fmt.Sprintf("%d", s.Port),
	}
	if s.Key != "" {
		args = append(args, "-i", s.Key)
	}
	args = append(args, fmt.Sprintf("%s@%s", s.User, s.Host), command)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("ssh: %s", strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("ssh: %w", err)
	}
	return stdout.String(), nil
}

// Copy copies a local file to the remote host via scp.
func (s *SSHSession) Copy(ctx context.Context, localPath, remotePath string) error {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-P", fmt.Sprintf("%d", s.Port),
	}
	if s.Key != "" {
		args = append(args, "-i", s.Key)
	}
	args = append(args, localPath, fmt.Sprintf("%s@%s:%s", s.User, s.Host, remotePath))

	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Probe checks if the remote host is reachable.
func (s *SSHSession) Probe(ctx context.Context) error {
	result, err := s.Run(ctx, "echo ok")
	if err != nil {
		return err
	}
	if strings.TrimSpace(result) != "ok" {
		return fmt.Errorf("unexpected probe response: %s", result)
	}
	return nil
}

// GetEnv returns a remote environment variable.
func (s *SSHSession) GetEnv(ctx context.Context, key string) (string, error) {
	return s.Run(ctx, fmt.Sprintf("echo $%s", key))
}
