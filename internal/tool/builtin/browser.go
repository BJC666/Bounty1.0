package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

// BrowserTool provides Chrome DevTools Protocol-based browser control.
// It launches a headless Chrome instance and communicates via CDP over HTTP.
type BrowserTool struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	debugURL string
	port     int
}

func (b *BrowserTool) Name() string   { return "browser" }
func (b *BrowserTool) ReadOnly() bool { return true }
func (b *BrowserTool) Description() string {
	return "Control a headless Chrome browser. Navigate pages, take screenshots, extract content."
}
func (b *BrowserTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["navigate","screenshot","content","click","type","start","stop"]},"url":{"type":"string","description":"URL for navigate action"},"selector":{"type":"string","description":"CSS selector for click/type"},"text":{"type":"string","description":"Text for type action"}},"required":["action"]}`)
}

func (b *BrowserTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action   string `json:"action"`
		URL      string `json:"url"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	switch params.Action {
	case "start":
		return b.start(ctx)
	case "stop":
		return b.stop()
	case "navigate":
		return b.navigate(ctx, params.URL)
	case "screenshot":
		return b.screenshot(ctx)
	case "content":
		return b.getContent(ctx)
	case "click":
		return b.click(ctx, params.Selector)
	case "type":
		return b.typeText(ctx, params.Selector, params.Text)
	default:
		return "", fmt.Errorf("unknown action: %s", params.Action)
	}
}

func (b *BrowserTool) start(ctx context.Context) (string, error) {
	if b.cmd != nil {
		return "Browser already running", nil
	}

	b.port = 9222
	chrome := findChrome()
	if chrome == "" {
		return "", fmt.Errorf("Chrome/Chromium not found. Install: brew install chromium (macOS) or apt install chromium-browser (Linux)")
	}

	b.cmd = exec.CommandContext(ctx, chrome,
		fmt.Sprintf("--remote-debugging-port=%d", b.port),
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--window-size=1280,720",
		"about:blank",
	)
	if err := b.cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start browser: %w", err)
	}

	// Wait for debug server
	time.Sleep(500 * time.Millisecond)
	b.debugURL = fmt.Sprintf("http://localhost:%d", b.port)
	return "Browser started (headless Chrome)", nil
}

func (b *BrowserTool) stop() (string, error) {
	if b.cmd == nil {
		return "Browser not running", nil
	}
	b.cmd.Process.Kill()
	b.cmd = nil
	b.debugURL = ""
	return "Browser stopped", nil
}

func (b *BrowserTool) navigate(ctx context.Context, url string) (string, error) {
	if b.debugURL == "" {
		return "", fmt.Errorf("browser not started. Use action=start first")
	}
	pageID, err := b.getPageID(ctx)
	if err != nil {
		return "", err
	}
	return b.sendCDP(ctx, pageID, "Page.navigate", map[string]string{"url": url})
}

func (b *BrowserTool) screenshot(ctx context.Context) (string, error) {
	if b.debugURL == "" {
		return "", fmt.Errorf("browser not started")
	}
	pageID, err := b.getPageID(ctx)
	if err != nil {
		return "", err
	}
	result, err := b.sendCDP(ctx, pageID, "Page.captureScreenshot", map[string]interface{}{
		"format": "png",
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Data string `json:"data"`
		} `json:"result"`
	}
	json.Unmarshal([]byte(result), &resp)
	if resp.Result.Data != "" {
		return "[Screenshot captured: " + fmt.Sprintf("%d bytes base64]", len(resp.Result.Data)), nil
	}
	return result, nil
}

func (b *BrowserTool) getContent(ctx context.Context) (string, error) {
	if b.debugURL == "" {
		return "", fmt.Errorf("browser not started")
	}
	pageID, err := b.getPageID(ctx)
	if err != nil {
		return "", err
	}
	return b.sendCDP(ctx, pageID, "Runtime.evaluate", map[string]interface{}{
		"expression":    "document.body.innerText",
		"returnByValue": true,
	})
}

func (b *BrowserTool) click(ctx context.Context, selector string) (string, error) {
	if b.debugURL == "" {
		return "", fmt.Errorf("browser not started")
	}
	pageID, err := b.getPageID(ctx)
	if err != nil {
		return "", err
	}
	return b.sendCDP(ctx, pageID, "Runtime.evaluate", map[string]interface{}{
		"expression":    fmt.Sprintf("document.querySelector(%q).click()", selector),
		"returnByValue": true,
	})
}

func (b *BrowserTool) typeText(ctx context.Context, selector, text string) (string, error) {
	if b.debugURL == "" {
		return "", fmt.Errorf("browser not started")
	}
	pageID, err := b.getPageID(ctx)
	if err != nil {
		return "", err
	}
	return b.sendCDP(ctx, pageID, "Runtime.evaluate", map[string]interface{}{
		"expression":    fmt.Sprintf("(function(){var e=document.querySelector(%q);e.value=%q;e.dispatchEvent(new Event('input'))})()", selector, text),
		"returnByValue": true,
	})
}

func (b *BrowserTool) getPageID(ctx context.Context) (string, error) {
	resp, err := http.Get(b.debugURL + "/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var pages []struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&pages)
	if len(pages) == 0 {
		return "", fmt.Errorf("no pages open")
	}
	return pages[0].ID, nil
}

func (b *BrowserTool) sendCDP(ctx context.Context, pageID, method string, params interface{}) (string, error) {
	body := map[string]interface{}{
		"id":     1,
		"method": method,
		"params": params,
	}
	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/json/protocol/%s", b.debugURL, pageID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result, _ := io.ReadAll(resp.Body)
	return string(result), nil
}

// findChrome locates an installed Chrome/Chromium browser.
func findChrome() string {
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "brave"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	// Windows paths
	for _, path := range []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
