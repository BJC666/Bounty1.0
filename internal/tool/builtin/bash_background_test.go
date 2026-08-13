package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// bgCommand returns a command that prints bg-start, sleeps ~seconds, then
// prints bg-done — portable across cmd.exe (Windows) and sh (CI Linux).
func bgCommand(seconds int) string {
	if cmdActive() {
		return "echo bg-start & ping -n " + itoa(seconds) + " 127.0.0.1 >nul & echo bg-done"
	}
	return "echo bg-start; sleep " + itoa(seconds) + "; echo bg-done"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func extractJobID(out string) string {
	i := strings.Index(out, "bg-")
	if i < 0 {
		return ""
	}
	j := i + 3
	for j < len(out) && out[j] >= "0"[0] && out[j] <= "9"[0] {
		j++
	}
	return out[i:j]
}

func bgBash() *BashTool {
	return &BashTool{Timeout: 120 * time.Second, Background: NewBackgroundStore()}
}

func callTool(t *testing.T, execer interface {
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return execer.Execute(context.Background(), raw)
}

func TestBackgroundJobPollAndExit(t *testing.T) {
	b := bgBash()
	out, err := callTool(t, b, map[string]any{
		"command": bgCommand(2), "description": "bg lifecycle",
		"run_in_background": true,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out, "bg-") || !strings.Contains(out, "bash_output") {
		t.Fatalf("start output should carry job id and polling hint: %q", out)
	}
	id := extractJobID(out)
	if id == "" {
		t.Fatalf("cannot extract job id from %q", out)
	}
	poll := &BashOutputTool{Store: b.Background}
	deadline := time.Now().Add(20 * time.Second)
	sawRunning := false
	done := ""
	for time.Now().Before(deadline) {
		o, err := callTool(t, poll, map[string]any{"job_id": id})
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if strings.Contains(o, "status: running") {
			sawRunning = true
		}
		if strings.Contains(o, "status: done") {
			done = o
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if done == "" {
		t.Fatalf("job never finished; last poll output missing")
	}
	if !strings.Contains(done, "exit_code: 0") {
		t.Fatalf("expected exit 0: %q", done)
	}
	if !strings.Contains(done, "bg-done") {
		t.Fatalf("expected bg-done marker in output: %q", done)
	}
	if !sawRunning {
		t.Log("note: job finished before first poll; running state not observed")
	}
}

func TestBackgroundAutoOnLongTimeout(t *testing.T) {
	b := bgBash()
	start := time.Now()
	out, err := callTool(t, b, map[string]any{
		"command": bgCommand(3), "description": "long", "timeout": 90000,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out, "bg-") {
		t.Fatalf("timeout>60s must auto-background: %q", out)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("background start should return immediately, took %s", elapsed)
	}
	// Cleanup: kill the still-running job.
	id := extractJobID(out)
	if _, err := callTool(t, &BashKillTool{Store: b.Background}, map[string]any{"job_id": id}); err != nil {
		t.Fatalf("kill: %v", err)
	}
}

func TestBashOutputUnknownJob(t *testing.T) {
	poll := &BashOutputTool{Store: NewBackgroundStore()}
	if _, err := callTool(t, poll, map[string]any{"job_id": "bg-999"}); err == nil {
		t.Fatal("expected error for unknown job id")
	}
}

func TestBashKillRunningJob(t *testing.T) {
	b := bgBash()
	long := "ping -n 30 127.0.0.1"
	if !cmdActive() {
		long = "sleep 30"
	}
	out, err := callTool(t, b, map[string]any{"command": long, "description": "kill me", "run_in_background": true})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := extractJobID(out)
	if id == "" {
		t.Fatalf("no job id in %q", out)
	}
	if _, err := callTool(t, &BashKillTool{Store: b.Background}, map[string]any{"job_id": id}); err != nil {
		t.Fatalf("kill: %v", err)
	}
	// Poll until the kill lands; the job must not keep running forever.
	poll := &BashOutputTool{Store: b.Background}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		o, err := callTool(t, poll, map[string]any{"job_id": id})
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if strings.Contains(o, "status: done") {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("job still running after kill")
}

func TestBackgroundFallsBackToSyncWithoutStore(t *testing.T) {
	b := &BashTool{Timeout: 120 * time.Second}
	out, err := callTool(t, b, map[string]any{
		"command": "echo sync-ok", "description": "no store", "run_in_background": true,
	})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if !strings.Contains(out, "sync-ok") {
		t.Fatalf("out=%q", out)
	}
}
