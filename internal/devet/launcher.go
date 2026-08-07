package devet

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Backend represents a connection to a DeVET verification backend,
// either pre-existing or started by this process.
type Backend struct {
	cmd     *exec.Cmd
	port    int
	baseURL string
}

// StartOrConnect tries to connect to an existing DeVET backend,
// or starts a new one if not running.
func StartOrConnect(ctx context.Context, devetDir string) (*Backend, error) {
	port := 8765
	baseURL := fmt.Sprintf("http://127.0.0.1:%d/api", port)

	// Try existing backend first
	if isRunning(baseURL) {
		return &Backend{port: port, baseURL: baseURL}, nil
	}

	// Try to start the Python backend
	serverPath := filepath.Join(devetDir, "backend", "server.py")
	if _, err := os.Stat(serverPath); err != nil {
		return nil, fmt.Errorf("DeVET backend not found at %s and not running — install DeVET first", serverPath)
	}

	pythonBin := "python3"
	if _, err := exec.LookPath("python3"); err != nil {
		pythonBin = "python"
	}
	cmd := exec.CommandContext(ctx, pythonBin, serverPath)
	cmd.Dir = filepath.Join(devetDir, "backend")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start DeVET backend: %w", err)
	}

	// Wait for backend to be ready
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		if isRunning(baseURL) {
			return &Backend{cmd: cmd, port: port, baseURL: baseURL}, nil
		}
	}
	cmd.Process.Kill()
	cmd.Wait()
	return nil, fmt.Errorf("DeVET backend started but not responding after 6s")
}

// devetClient carries a short timeout so a stalled local backend cannot hang
// the readiness probe.
var devetClient = &http.Client{Timeout: 2 * time.Second}

func isRunning(baseURL string) bool {
	resp, err := devetClient.Get(baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// BaseURL returns the API base URL for this backend connection.
func (b *Backend) BaseURL() string { return b.baseURL }

// Stop terminates the backend process if one was started by us.
func (b *Backend) Stop() {
	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Kill()
		b.cmd.Wait()
	}
}
