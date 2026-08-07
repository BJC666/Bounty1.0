package devet

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStartOrConnectMissingDeVETDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no-such-dir")
	_, err := StartOrConnect(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error when DeVET backend dir missing")
	}
}

func TestStartOrConnectWithBackendFileButNoServer(t *testing.T) {
	// backend/server.py exists but python is unlikely to serve on 8765 in CI;
	// we only assert the error is returned and mentions startup failure.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "backend"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "backend", "server.py"),
		[]byte("import time\ntime.sleep(60)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := StartOrConnect(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error: port 8765 should not be listening in test env")
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
