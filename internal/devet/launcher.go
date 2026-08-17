package devet

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Backend represents a connection to a DeVET verification backend,
// either pre-existing or started by this process.
type Backend struct {
	cmd     *exec.Cmd
	port    int
	baseURL string
}

// Tunable so tests can shorten the readiness window without slowing the suite.
var (
	backendReadyTimeout      = 10 * time.Second
	backendReadyPollInterval = 200 * time.Millisecond
)

// StartOrConnect tries to connect to an existing DeVET backend,
// or starts a new one if not running.
//
// devetDir may be relative (e.g. "..\DeVET"); it is resolved to an absolute
// path before use because the child python process runs with its working
// directory set to <devetDir>/backend — a relative server path in argv would
// be re-resolved against that directory and double up into a non-existent
// file (the historical "not responding after 6s" bug).
func StartOrConnect(ctx context.Context, devetDir string) (*Backend, error) {
	port := 8765
	if p := os.Getenv("DEVET_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65536 {
			port = n
		}
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d/api", port)

	// Try existing backend first
	if isRunning(baseURL) {
		return &Backend{port: port, baseURL: baseURL}, nil
	}

	absDevetDir, err := filepath.Abs(devetDir)
	if err != nil {
		return nil, fmt.Errorf("resolve DeVET dir %q: %w", devetDir, err)
	}

	// 方案 A（P9）：优先使用 PyInstaller 打包的 sidecar（devet-server.exe），
	// 无需本机 Python 环境；找不到或启动失败时回退到 python server.py。
	if exe := findSidecarServer(absDevetDir); exe != "" {
		cmd := exec.CommandContext(ctx, exe)
		// PyInstaller onefile 是 bootloader 父进程 + 实际服务子进程；若用 Go
		// 管道接 stdout/stderr，孙进程持有管道写端会让 cmd.Wait() 永久挂起。
		// 直接指向 /dev/null（*os.File 直连，不建管道），失败时按进程树整杀。
		cmd.Stdout = devNull()
		cmd.Stderr = devNull()
		if err := cmd.Start(); err == nil {
			if waitReady(baseURL) {
				return &Backend{cmd: cmd, port: port, baseURL: baseURL}, nil
			}
			killProcessTree(cmd.Process.Pid)
			_ = cmd.Wait()
			return nil, fmt.Errorf("DeVET sidecar %s started but not responding after %s",
				exe, backendReadyTimeout)
		}
		// sidecar 无法启动（被杀软/权限等）→ 回退 python 路径
	}

	backendDir := filepath.Join(absDevetDir, "backend")
	serverPath := filepath.Join(backendDir, "server.py")
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
	cmd.Dir = backendDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start DeVET backend: %w", err)
	}

	if !waitReady(baseURL) {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("DeVET backend started but not responding after %s: %s",
			backendReadyTimeout, tail(stderr.String(), 500))
	}
	return &Backend{cmd: cmd, port: port, baseURL: baseURL}, nil
}

// findSidecarServer locates a PyInstaller-packaged DeVET backend executable.
// Search order: $DEVET_SERVER -> the directory of the running bounty binary
// -> <devetDir>/dist/devet-server.exe.
func findSidecarServer(devetDir string) string {
	if p := os.Getenv("DEVET_SERVER"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "devet-server.exe")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	candidate := filepath.Join(devetDir, "dist", "devet-server.exe")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate
	}
	return ""
}

// waitReady polls the backend health endpoint until it answers or the
// readiness window elapses.
func waitReady(baseURL string) bool {
	deadline := time.Now().Add(backendReadyTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(backendReadyPollInterval)
		if isRunning(baseURL) {
			return true
		}
	}
	return false
}

// probePython verifies a python candidate actually runs (guards against the
// Windows Store python3.exe alias stub, which exists on PATH but exits 9009).
func probePython(bin string) bool {
	out, err := exec.Command(bin, "--version").CombinedOutput()
	return err == nil && len(out) > 0
}

// tail returns the last n characters of s, useful for error diagnostics.
func tail(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
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

// Stop terminates the backend process tree if one was started by us
// (onefile bootloader + actual server child on Windows).
func (b *Backend) Stop() {
	if b.cmd != nil && b.cmd.Process != nil {
		killProcessTree(b.cmd.Process.Pid)
		_ = b.cmd.Wait()
	}
}

// devNull returns an *os.File bound to the null device, used as the sidecar's
// stdout/stderr so cmd.Wait() never blocks on inherited pipe handles.
func devNull() *os.File {
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	return f
}

// killProcessTree terminates pid and (on Windows) its entire process tree.
// PyInstaller onefile spawns a server child; plain Kill would orphan it and
// leave the port occupied.
func killProcessTree(pid int) {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
