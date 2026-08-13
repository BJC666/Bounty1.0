package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"bounty/internal/sandbox"
	"bounty/internal/tool"
)

// maxBackgroundJobs caps in-process background jobs so a runaway agent cannot
// fork the machine to death; bash returns a clear error once the cap is hit.
const maxBackgroundJobs = 16

// BackgroundJob is one detached shell command. Output is captured in a log
// file under the store directory; Poll reads its tail.
type BackgroundJob struct {
	ID        string
	Command   string
	Started   time.Time
	LogPath   string
	cmd       *exec.Cmd
	container *sandbox.Container

	mu       sync.Mutex
	done     bool
	exitCode int
	runErr   error
}

// BackgroundStore keeps in-process background jobs keyed by id. Jobs only
// live as long as the bounty process (documented limitation of the P4-3
// MVP; persistent jobs across restarts are out of scope).
type BackgroundStore struct {
	mu   sync.Mutex
	seq  int
	jobs map[string]*BackgroundJob
	dir  string
}

// NewBackgroundStore creates a store with its log directory under the system
// temp dir.
func NewBackgroundStore() *BackgroundStore {
	dir, err := os.MkdirTemp("", "bounty-bg")
	if err != nil {
		dir = os.TempDir()
	}
	return &BackgroundStore{jobs: map[string]*BackgroundJob{}, dir: dir}
}

// Start registers and launches cmd. When container is non-nil the process
// was already started by SandboxStart (Windows Job Object path); otherwise
// Start calls cmd.Start itself. Returns the job id.
func (s *BackgroundStore) Start(cmd *exec.Cmd, container *sandbox.Container, startErr error, command string) (string, error) {
	if startErr != nil {
		return "", startErr
	}
	s.mu.Lock()
	if len(s.jobs) >= maxBackgroundJobs {
		s.mu.Unlock()
		return "", fmt.Errorf("后台任务已达上限（%d 个），先等待已有任务结束", maxBackgroundJobs)
	}
	s.seq++
	id := fmt.Sprintf("bg-%d", s.seq)
	logPath := filepath.Join(s.dir, id+".log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	job := &BackgroundJob{
		ID: id, Command: command, Started: time.Now(), LogPath: logPath,
		cmd: cmd, container: container,
	}
	s.jobs[id] = job
	s.mu.Unlock()

	cmd.Stdout = logf
	cmd.Stderr = logf
	if container == nil {
		if err := cmd.Start(); err != nil {
			logf.Close()
			s.mu.Lock()
			delete(s.jobs, id)
			s.mu.Unlock()
			return "", err
		}
	}
	go func() {
		waitErr := cmd.Wait()
		logf.Close()
		job.mu.Lock()
		job.done = true
		job.runErr = waitErr
		if waitErr != nil {
			if ee, ok := waitErr.(*exec.ExitError); ok {
				job.exitCode = ee.ExitCode()
			} else {
				job.exitCode = -1
			}
		}
		job.mu.Unlock()
		if job.container != nil {
			job.container.Close()
		}
	}()
	return id, nil
}

// Poll returns the job's latest output tail and status. maxRunes caps the
// returned output (tail of the log, so the agent sees the most recent
// progress).
func (s *BackgroundStore) Poll(id string, maxRunes int) (running bool, exitCode int, output string, err error) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	s.mu.Unlock()
	if !ok {
		return false, 0, "", fmt.Errorf("未知的后台任务 %s（进程内任务列表为空或已清理）", id)
	}
	if maxRunes <= 0 {
		maxRunes = 8000
	}
	if maxRunes > 16000 {
		maxRunes = 16000
	}
	raw, readErr := readLogTail(job.LogPath)
	output = decodeOutput(raw)
	output = tailRunes(output, maxRunes)

	job.mu.Lock()
	defer job.mu.Unlock()
	if job.done {
		return false, job.exitCode, output, nil
	}
	if readErr != nil && os.IsNotExist(readErr) {
		output = ""
	}
	return true, 0, output, nil
}

// Kill terminates a running job (whole process tree on Windows). Finished
// jobs are a no-op.
func (s *BackgroundStore) Kill(id string) (string, error) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("未知的后台任务 %s", id)
	}
	job.mu.Lock()
	done := job.done
	job.mu.Unlock()
	if done {
		return fmt.Sprintf("任务 %s 已结束（exit_code=%d），无需终止", job.ID, job.exitCode), nil
	}
	if job.container != nil {
		_ = job.container.Kill()
	} else if job.cmd != nil && job.cmd.Process != nil {
		killProcessTree(job.cmd.Process.Pid)
		_ = job.cmd.Process.Kill()
	}
	return fmt.Sprintf("已终止后台任务 %s", job.ID), nil
}

// readLogTail returns the last bytes of the log (bounded) so polling a huge
// build log stays cheap.
func readLogTail(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const maxRead = 256 * 1024
	start := st.Size() - maxRead
	if start < 0 {
		start = 0
	}
	buf := make([]byte, st.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil {
		return nil, err
	}
	return buf, nil
}

// tailRunes keeps the last n runes of s, with a truncation notice.
func tailRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return fmt.Sprintf("[输出较长，仅显示最后 %d 字符]\n%s", n, string(runes[len(runes)-n:]))
}

// BashOutputTool polls a background job started by bash (run_in_background
// or timeout > 60s).
type BashOutputTool struct {
	Store *BackgroundStore
}

func (b *BashOutputTool) Name() string      { return "bash_output" }
func (b *BashOutputTool) ReadOnly() bool    { return true }
func (b *BashOutputTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (b *BashOutputTool) Description() string {
	return "Poll a background bash job started with run_in_background=true or timeout>60s. Returns status (running/done), exit code, and the latest output tail."
}
func (b *BashOutputTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string","maxLength":64,"description":"Job id returned by bash (e.g. bg-1)"},"tail":{"type":"number","minimum":100,"maximum":16000,"description":"Max output characters to return (default 8000)"}},"required":["job_id"],"additionalProperties":false}`)
}
func (b *BashOutputTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if b.Store == nil {
		return "", fmt.Errorf("bash_output 不可用（未配置后台任务存储）")
	}
	var params struct {
		JobID string  `json:"job_id"`
		Tail  float64 `json:"tail"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if params.JobID == "" {
		return "", fmt.Errorf("job_id 必填")
	}
	running, code, output, err := b.Store.Poll(params.JobID, int(params.Tail))
	if err != nil {
		return "", err
	}
	if running {
		return fmt.Sprintf("status: running\n%s", output), nil
	}
	return fmt.Sprintf("status: done\nexit_code: %d\n%s", code, output), nil
}

// BashKillTool terminates a background job.
type BashKillTool struct {
	Store *BackgroundStore
}

func (b *BashKillTool) Name() string      { return "bash_kill" }
func (b *BashKillTool) ReadOnly() bool    { return false }
func (b *BashKillTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (b *BashKillTool) Description() string {
	return "Terminate a running background bash job by job id."
}
func (b *BashKillTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string","maxLength":64,"description":"Job id returned by bash (e.g. bg-1)"}},"required":["job_id"],"additionalProperties":false}`)
}
func (b *BashKillTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if b.Store == nil {
		return "", fmt.Errorf("bash_kill 不可用（未配置后台任务存储）")
	}
	var params struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if params.JobID == "" {
		return "", fmt.Errorf("job_id 必填")
	}
	return b.Store.Kill(params.JobID)
}
