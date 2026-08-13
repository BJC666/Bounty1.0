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

	// On Windows, python3.exe may be a Store stub that LookPath finds but
	// cannot actually execute; probe each candidate with --version and pick
	// the first real interpreter.
	pythonBin := ""
	for _, candidate := range []string{"python", "python3"} {
		if path, err := exec.LookPath(candidate); err == nil && probePython(path) {
			pythonBin = path
			break
		}
	}
	if pythonBin == "" {
		return nil, fmt.Errorf("python 3 not found on PATH — install Python to use DeVET tools")
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

// probePython verifies a python candidate actually runs (guards against the
// Windows Store python3.exe alias stub, which exists on PATH but exits 9009).
func probePython(bin string) bool {
	out, err := exec.Command(bin, "--version").CombinedOutput()
	return err == nil && len(out) > 0
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
