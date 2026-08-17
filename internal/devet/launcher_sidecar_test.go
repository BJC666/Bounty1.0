package devet

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFindSidecarServerEnv verifies $DEVET_SERVER is the first search hit.
func TestFindSidecarServerEnv(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "devet-server.exe")
	if err := os.WriteFile(fake, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVET_SERVER", fake)
	if got := findSidecarServer(t.TempDir()); got != fake {
		t.Fatalf("findSidecarServer = %q, want %q", got, fake)
	}
}

// TestFindSidecarServerDist verifies the <devetDir>/dist fallback hit.
func TestFindSidecarServerDist(t *testing.T) {
	devetDir := t.TempDir()
	dist := filepath.Join(devetDir, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dist, "devet-server.exe")
	if err := os.WriteFile(fake, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVET_SERVER", "") // ensure env override is absent
	if got := findSidecarServer(devetDir); got != fake {
		t.Fatalf("findSidecarServer = %q, want %q", got, fake)
	}
}

// TestFindSidecarServerPriority verifies $DEVET_SERVER beats the dist path.
func TestFindSidecarServerPriority(t *testing.T) {
	devetDir := t.TempDir()
	dist := filepath.Join(devetDir, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "devet-server.exe"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(t.TempDir(), "other.exe")
	if err := os.WriteFile(env, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVET_SERVER", env)
	if got := findSidecarServer(devetDir); got != env {
		t.Fatalf("findSidecarServer = %q, want %q", got, env)
	}
}

// TestStartOrConnectSidecar does an end-to-end sidecar launch when a real
// PyInstaller bundle exists (set DEVET_SERVER_TEST to its path). Skipped
// otherwise so CI and non-Windows runs stay green.
func TestStartOrConnectSidecar(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only sidecar e2e")
	}
	exe := os.Getenv("DEVET_SERVER_TEST")
	if exe == "" {
		t.Skip("set DEVET_SERVER_TEST=<path-to-devet-server.exe> to run sidecar e2e")
	}
	t.Setenv("DEVET_SERVER", exe)
	t.Setenv("DEVET_PORT", "8799")
	backend, err := StartOrConnect(context.Background(), ".")
	if err != nil {
		t.Fatalf("StartOrConnect: %v", err)
	}
	defer backend.Stop()
	if backend.cmd == nil {
		t.Fatal("expected a self-started sidecar process, got an existing backend")
	}
	if !isRunning(backend.BaseURL()) {
		t.Fatalf("sidecar backend not healthy at %s", backend.BaseURL())
	}
}
