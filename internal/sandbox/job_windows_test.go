//go:build windows

package sandbox

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestJobCloseKillsChildTree proves the core containment property: closing
// the job handle terminates the direct child and its descendants.
func TestJobCloseKillsChildTree(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	cont, err := StartContained(cmd, JobOptions{Network: true})
	if err != nil {
		t.Fatalf("StartContained: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	start := time.Now()
	if err := cont.Close(); err != nil {
		t.Fatalf("container close: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not die within 5s after job close")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("kill took %s, job close did not reap children", elapsed)
	}
}

// TestJobCloseReapsPingChild checks via tasklist that the ping.exe child
// spawned by cmd.exe is gone after the job handle closes.
func TestJobCloseReapsPingChild(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	cont, err := StartContained(cmd, JobOptions{Network: true})
	if err != nil {
		t.Fatalf("StartContained: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if err := cont.Close(); err != nil {
		t.Fatalf("container close: %v", err)
	}
	_ = cmd.Wait()
	time.Sleep(300 * time.Millisecond)
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq ping.exe", "/NH").CombinedOutput()
	if err != nil {
		t.Fatalf("tasklist: %v", err)
	}
	if strings.Contains(strings.ToLower(string(out)), "ping.exe") {
		t.Fatalf("ping.exe survived job close")
	}
}

// TestJobStripsSecretsAndBlocksProxyWhenNetworkOff checks env handling.
func TestJobStripsSecretsAndBlocksProxyWhenNetworkOff(t *testing.T) {
	os.Setenv("DEEPSEEK_API_KEY", "secret-value")
	defer os.Unsetenv("DEEPSEEK_API_KEY")

	cmd := exec.Command("cmd", "/c", "echo", "hi")
	cont, err := StartContained(cmd, JobOptions{Network: false})
	if err != nil {
		t.Fatalf("StartContained: %v", err)
	}
	defer cont.Close()
	_ = cmd.Wait()

	env := map[string]string{}
	for _, e := range cmd.Env {
		if i := strings.IndexByte(e, '='); i >= 0 {
			env[e[:i]] = e[i+1:]
		}
	}
	if _, leaked := env["DEEPSEEK_API_KEY"]; leaked {
		t.Fatal("API key leaked into contained env")
	}
	if !strings.Contains(env["HTTP_PROXY"], "127.0.0.1:9") {
		t.Fatalf("HTTP_PROXY not poisoned: %q", env["HTTP_PROXY"])
	}
	if env["NO_PROXY"] != "" {
		t.Fatalf("NO_PROXY must be empty: %q", env["NO_PROXY"])
	}
}

// TestJobNetworkOnKeepsProxy checks network=true leaves proxy env untouched.
func TestJobNetworkOnKeepsProxy(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "echo", "hi")
	cont, err := StartContained(cmd, JobOptions{Network: true})
	if err != nil {
		t.Fatalf("StartContained: %v", err)
	}
	defer cont.Close()
	_ = cmd.Wait()
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "HTTP_PROXY=http://127.0.0.1:9") {
			t.Fatalf("proxy poisoned while network=true: %s", e)
		}
	}
}
