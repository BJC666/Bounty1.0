package builtin

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestBrowserSmokeCDP exercises the browser tool end-to-end against a real
// Chrome instance. Skipped unless BOUNTY_BROWSER_TEST=1 is set.
func TestBrowserSmokeCDP(t *testing.T) {
	if os.Getenv("BOUNTY_BROWSER_TEST") != "1" {
		t.Skip("set BOUNTY_BROWSER_TEST=1 to run against real Chrome")
	}
	if findChrome() == "" {
		t.Skip("Chrome not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	b := &BrowserTool{}
	out, err := b.Execute(ctx, json.RawMessage(`{"action":"start"}`))
	t.Logf("start: %s err=%v", out, err)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer b.stop()

	out, err = b.Execute(ctx, json.RawMessage(`{"action":"navigate","url":"https://example.com"}`))
	t.Logf("navigate: %s err=%v", out, err)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}
	time.Sleep(3 * time.Second)

	out, err = b.Execute(ctx, json.RawMessage(`{"action":"content"}`))
	t.Logf("content: %s err=%v", truncate(out, 300), err)
	if err != nil {
		t.Fatalf("content failed: %v", err)
	}

	out, err = b.Execute(ctx, json.RawMessage(`{"action":"screenshot"}`))
	t.Logf("screenshot: %s err=%v", out, err)
	if err != nil {
		t.Fatalf("screenshot failed: %v", err)
	}

	out, err = b.Execute(ctx, json.RawMessage(`{"action":"click","selector":"h1"}`))
	t.Logf("click: %s err=%v", out, err)

	out, err = b.Execute(ctx, json.RawMessage(`{"action":"stop"}`))
	t.Logf("stop: %s err=%v", out, err)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
