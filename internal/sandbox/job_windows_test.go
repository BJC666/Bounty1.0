//go:build windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// spawned by cmd.exe is gone after the job handle closes. The child runs
// from a uniquely named copy of ping.exe so that parallel test packages
// (e.g. builtin's own ping-based timeout test) can never cause a false
// positive.
func TestJobCloseReapsPingChild(t *testing.T) {
	pingPath, err := exec.LookPath("ping")
	if err != nil {
		t.Skipf("ping not on PATH: %v", err)
	}
	data, err := os.ReadFile(pingPath)
	if err != nil {
		t.Fatalf("read ping.exe: %v", err)
	}
	unique := filepath.Join(os.TempDir(), fmt.Sprintf("bounty-ping-%d.exe", os.Getpid()))
	if err := os.WriteFile(unique, data, 0o755); err != nil {
		t.Fatalf("write unique ping copy: %v", err)
	}
	defer os.Remove(unique)
	image := filepath.Base(unique)

	cmd := exec.Command("cmd", "/c", unique, "-n", "30", "127.0.0.1")
	cont, err := StartContained(cmd, JobOptions{Network: true})
	if err != nil {
		t.Fatalf("StartContained: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !tasklistHasImage(t, image) {
		if time.Now().After(deadline) {
			cont.Close()
			_ = cmd.Wait()
			t.Fatalf("contained child %s never spawned", image)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("container close: %v", err)
	}
	_ = cmd.Wait()
	deadline = time.Now().Add(3 * time.Second)
	for tasklistHasImage(t, image) {
		if time.Now().After(deadline) {
			t.Fatalf("%s survived job close", image)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func tasklistHasImage(t *testing.T, image string) bool {
	t.Helper()
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+image, "/NH").CombinedOutput()
	if err != nil {
		t.Fatalf("tasklist: %v", err)
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(image))
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
