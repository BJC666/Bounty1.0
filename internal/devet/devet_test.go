package devet

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return strconv.Itoa(port)
}

func shortWindow(t *testing.T) {
	t.Helper()
	old := backendReadyTimeout
	backendReadyTimeout = 500 * time.Millisecond
	t.Cleanup(func() { backendReadyTimeout = old })
}

func TestStartOrConnectMissingDeVETDir(t *testing.T) {
	t.Setenv("DEVET_PORT", freePort(t))
	dir := filepath.Join(t.TempDir(), "no-such-dir")
	_, err := StartOrConnect(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error when DeVET backend dir missing")
	}
}

func TestStartOrConnectWithBackendFileButNoServer(t *testing.T) {
	// backend/server.py exists but never serves; we only assert the error is
	// returned and mentions startup failure. A dedicated port keeps this
	// deterministic even if a real DeVET backend is running on 8765.
	t.Setenv("DEVET_PORT", freePort(t))
	shortWindow(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "backend"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "backend", "server.py"),
		[]byte("import time\ntime.sleep(60)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := StartOrConnect(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error: fake backend never listens")
	}
}

func TestStartOrConnectStartsRealBackend(t *testing.T) {
	// Regression test for the relative-path doubling bug: with a real DeVET
	// checkout next to the Bounty repo, the launcher must bring up uvicorn
	// and serve /api/health. Skipped when no backend source is available
	// (e.g. CI without the DeVET checkout).
	devetDir := filepath.Join("..", "..", "..", "DeVET")
	if _, err := os.Stat(filepath.Join(devetDir, "backend", "server.py")); err != nil {
		t.Skip("DeVET checkout not available next to repo")
	}
	port := freePort(t)
	t.Setenv("DEVET_PORT", port)
	old := backendReadyTimeout
	backendReadyTimeout = 15 * time.Second
	defer func() { backendReadyTimeout = old }()

	b, err := StartOrConnect(context.Background(), devetDir)
	if err != nil {
		t.Fatalf("StartOrConnect: %v", err)
	}
	defer b.Stop()

	resp, err := http.Get("http://127.0.0.1:" + port + "/api/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
}

func TestBackendBaseURL(t *testing.T) {
	b := &Backend{port: 8765, baseURL: "http://127.0.0.1:8765/api"}
	if b.BaseURL() != "http://127.0.0.1:8765/api" {
		t.Fatalf("BaseURL = %q", b.BaseURL())
	}
}

func TestStopWithoutProcessIsSafe(t *testing.T) {
	b := &Backend{}
	b.Stop() // must not panic when cmd is nil
}
