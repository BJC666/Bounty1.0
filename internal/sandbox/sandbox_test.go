package sandbox

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestWrapSetsWorkingDir(t *testing.T) {
	cmd := exec.Command("pwd")
	Wrap(cmd, `C:\workspace`)
	if cmd.Dir != `C:\workspace` {
		t.Fatalf("Dir = %q, want C:\\workspace", cmd.Dir)
	}
}

func TestWrapKeepsDirWhenRootEmpty(t *testing.T) {
	cmd := exec.Command("pwd")
	cmd.Dir = "/original"
	Wrap(cmd, "")
	if cmd.Dir != "/original" {
		t.Fatalf("Dir = %q, want /original untouched", cmd.Dir)
	}
}

func TestWrapStripsAPIKeys(t *testing.T) {
	for _, key := range []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY",
		"DEEPSEEK_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	} {
		os.Setenv(key, "super-secret-value")
		defer os.Unsetenv(key)
	}
	os.Setenv("PATH", os.Getenv("PATH"))

	cmd := exec.Command("echo", "hi")
	Wrap(cmd, "")
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") ||
			strings.HasPrefix(e, "OPENAI_API_KEY=") ||
			strings.HasPrefix(e, "DEEPSEEK_API_KEY=") ||
			strings.HasPrefix(e, "ANTHROPIC_AUTH_TOKEN=") {
			t.Fatalf("API key leaked into env: %s", e)
		}
	}
}

func TestWrapKeepsOtherEnvVars(t *testing.T) {
	os.Setenv("BOUNTY_TEST_MARKER", "keep-me")
	defer os.Unsetenv("BOUNTY_TEST_MARKER")
	cmd := exec.Command("echo", "hi")
	Wrap(cmd, "")
	found := false
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "BOUNTY_TEST_MARKER=") {
			found = true
		}
	}
	if !found {
		t.Fatal("non-secret env var was dropped by Wrap")
	}
}

func TestNewDockerSandboxDefaults(t *testing.T) {
	d := NewDockerSandbox("", "/host/ws")
	if d.Image != "alpine:3.21" {
		t.Fatalf("Image = %q, want alpine:3.21", d.Image)
	}
	if d.WorkDir != "/workspace" {
		t.Fatalf("WorkDir = %q, want /workspace", d.WorkDir)
	}
	if d.HostDir != "/host/ws" {
		t.Fatalf("HostDir = %q, want /host/ws", d.HostDir)
	}
	if d.NetworkOff || d.ReadOnly {
		t.Fatal("default sandbox must have network on and writable")
	}
}

func TestWrapInDockerEscapesQuotes(t *testing.T) {
	d := NewDockerSandbox("python:3.12", "/host")
	got := d.WrapInDocker("echo 'it''s fine'")
	if n := strings.Count(got, "'\\''"); n != 4 {
		t.Fatalf("expected 3 escaped quotes, got %d in: %s", n, got)
	}
}

func TestWrapInDockerVolumeMount(t *testing.T) {
	d := NewDockerSandbox("", "/host/ws")
	got := d.WrapInDocker("ls")
	if !strings.Contains(got, "-v /host/ws:/workspace") {
		t.Fatalf("expected host volume mount, got: %s", got)
	}
}

func TestWrapInDockerDefaultImage(t *testing.T) {
	d := NewDockerSandbox("", "")
	got := d.WrapInDocker("ls")
	if !strings.Contains(got, "alpine:3.21") {
		t.Fatalf("expected default alpine image, got: %s", got)
	}
}
