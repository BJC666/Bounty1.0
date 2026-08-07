package builtin

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"bounty/internal/tool"
)

// BrowserTool provides Chrome DevTools Protocol-based browser control.
// It launches a headless Chrome instance and communicates via CDP over HTTP.
type BrowserTool struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	debugURL string
	port     int
	dataDir  string // temp Chrome profile (cleaned up on stop)
}

func (b *BrowserTool) Name() string   { return "browser" }
func (b *BrowserTool) ReadOnly() bool { return true }
func (b *BrowserTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
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

	// Refuse to start if another instance already owns the debug port —
	// otherwise commands would silently talk to the wrong browser.
	if resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json/version", b.port)); err == nil {
		resp.Body.Close()
		return "", fmt.Errorf("port %d already in use by another browser instance", b.port)
	}

	// Use a dedicated temp profile so installed extensions (which show up as
	// extra CDP targets) cannot pollute the page list.
	dataDir, err := os.MkdirTemp("", "bounty-chrome-")
	if err != nil {
		return "", fmt.Errorf("create browser profile: %w", err)
	}
	b.dataDir = dataDir

	b.cmd = exec.CommandContext(ctx, chrome,
		fmt.Sprintf("--remote-debugging-port=%d", b.port),
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-extensions",
		fmt.Sprintf("--user-data-dir=%s", dataDir),
		"--window-size=1280,720",
		"about:blank",
	)
	if err := b.cmd.Start(); err != nil {
		os.RemoveAll(dataDir)
		b.dataDir = ""
		return "", fmt.Errorf("failed to start browser: %w", err)
	}

	// Wait for the DevTools server to accept connections (up to 5s).
	b.debugURL = fmt.Sprintf("http://localhost:%d", b.port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(b.debugURL + "/json/version")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			b.cmd.Process.Kill()
			b.cmd.Wait()
			b.cmd = nil
			b.debugURL = ""
			os.RemoveAll(dataDir)
			b.dataDir = ""
			return "", fmt.Errorf("Chrome DevTools did not start within 5s: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "Browser started (headless Chrome)", nil
}

func (b *BrowserTool) stop() (string, error) {
	if b.cmd == nil {
		return "Browser not running", nil
	}
	b.cmd.Process.Kill()
	b.cmd.Wait()
	b.cmd = nil
	b.debugURL = ""
	if b.dataDir != "" {
		os.RemoveAll(b.dataDir)
		b.dataDir = ""
	}
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

// cdpTarget mirrors the /json target list entries.
type cdpTarget struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Type string `json:"type"`
	WS   string `json:"webSocketDebuggerUrl"`
}

func (b *BrowserTool) listTargets(ctx context.Context) ([]cdpTarget, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", b.debugURL+"/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var pages []cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return nil, fmt.Errorf("decode page list: %w", err)
	}
	return pages, nil
}

// pickTarget returns the first driveable page target. Extension background
// pages and other non-page targets are skipped so commands never land on an
// undriveable target.
func pickTarget(pages []cdpTarget) (cdpTarget, bool) {
	for _, p := range pages {
		if p.Type != "" && p.Type != "page" {
			continue
		}
		if strings.HasPrefix(p.URL, "chrome-extension://") {
			continue
		}
		if p.WS != "" {
			return p, true
		}
	}
	for _, p := range pages {
		if p.WS != "" {
			return p, true
		}
	}
	return cdpTarget{}, false
}

func (b *BrowserTool) getPageID(ctx context.Context) (string, error) {
	pages, err := b.listTargets(ctx)
	if err != nil {
		return "", err
	}
	t, ok := pickTarget(pages)
	if !ok {
		return "", fmt.Errorf("no pages open")
	}
	return t.ID, nil
}

// getPageWSURL resolves the page's webSocketDebuggerUrl from the /json list.
// CDP commands must be sent over this WebSocket — the HTTP /json/protocol
// endpoint used previously does not exist.
func (b *BrowserTool) getPageWSURL(ctx context.Context, pageID string) (string, error) {
	pages, err := b.listTargets(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range pages {
		if p.ID == pageID && p.WS != "" {
			return p.WS, nil
		}
	}
	t, ok := pickTarget(pages)
	if !ok {
		return "", fmt.Errorf("no websocket debugger url for page %q", pageID)
	}
	return t.WS, nil
}

func (b *BrowserTool) sendCDP(ctx context.Context, pageID, method string, params interface{}) (string, error) {
	wsURL, err := b.getPageWSURL(ctx, pageID)
	if err != nil {
		return "", err
	}
	conn, err := dialWS(ctx, wsURL)
	if err != nil {
		return "", fmt.Errorf("connect CDP: %w", err)
	}
	defer conn.Close()

	msgID := 1
	body := map[string]interface{}{
		"id":     msgID,
		"method": method,
		"params": params,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	if err := conn.sendText(data); err != nil {
		return "", fmt.Errorf("send CDP command: %w", err)
	}
	conn.conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	for {
		opcode, payload, err := conn.readFrame()
		if err != nil {
			return "", err
		}
		if opcode != wsText {
			continue
		}
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue // not a JSON command response — ignore
		}
		if msg.ID != msgID {
			continue // CDP event or response to another command
		}
		if msg.Error != nil {
			return "", fmt.Errorf("CDP %s error %d: %s", method, msg.Error.Code, msg.Error.Message)
		}
		return string(msg.Result), nil
	}
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

// ── minimal WebSocket client (RFC 6455) for the Chrome DevTools Protocol ──
// CDP commands must be sent over a WebSocket (the HTTP /json/protocol
// endpoint does not exist), so Bounty ships a tiny ws client instead of
// pulling in an external dependency.

const (
	wsText  = 0x1
	wsClose = 0x8
	wsPing  = 0x9
	wsPong  = 0xA
)

type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// dialWS performs the HTTP Upgrade handshake against a ws:// URL.
func dialWS(ctx context.Context, rawURL string) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported websocket scheme %q", u.Scheme)
	}
	addr := u.Host
	if u.Port() == "" {
		addr += ":80"
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, u.Host, key,
	)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, " 101 ") {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake failed: %s", strings.TrimSpace(status))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &wsConn{conn: conn, br: br}, nil
}

// sendText writes a masked text frame with the given payload. Client frames
// are always masked per RFC 6455.
func (w *wsConn) sendText(payload []byte) error {
	mask := make([]byte, 4)
	rand.Read(mask)

	var header []byte
	n := len(payload)
	switch {
	case n < 126:
		header = []byte{0x81, 0x80 | byte(n)}
	case n <= 0xFFFF:
		header = []byte{0x81, 0x80 | 126, byte(n >> 8), byte(n)}
	default:
		header = make([]byte, 10)
		header[0] = 0x81
		header[1] = 0x80 | 127
		binary.BigEndian.PutUint64(header[2:], uint64(n))
	}
	frame := make([]byte, 0, len(header)+4+n)
	frame = append(frame, header...)
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := w.conn.Write(frame)
	return err
}

// writeControl sends a small control frame (ping/pong).
func (w *wsConn) writeControl(opcode byte, payload []byte) error {
	mask := make([]byte, 4)
	rand.Read(mask)
	frame := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := w.conn.Write(frame)
	return err
}

// readFrame reads a complete data message, transparently replying to pings
// and reassembling fragmented messages. Server frames are unmasked.
func (w *wsConn) readFrame() (byte, []byte, error) {
	var msg []byte
	msgOpcode := byte(0)
	for {
		var hdr [2]byte
		if _, err := io.ReadFull(w.br, hdr[:]); err != nil {
			return 0, nil, err
		}
		fin := hdr[0]&0x80 != 0
		opcode := hdr[0] & 0x0F
		masked := hdr[1]&0x80 != 0
		length := uint64(hdr[1] & 0x7F)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return 0, nil, err
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(w.br, maskKey[:]); err != nil {
				return 0, nil, err
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(w.br, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= maskKey[i%4]
			}
		}
		switch opcode {
		case wsPing:
			w.writeControl(wsPong, payload)
			continue
		case wsPong:
			continue
		case 0x0: // continuation frame
			msg = append(msg, payload...)
			if fin {
				return msgOpcode, msg, nil
			}
			continue
		default:
			msgOpcode = opcode
			msg = append(msg[:0], payload...)
			if fin {
				return opcode, msg, nil
			}
		}
	}
}

// Close closes the underlying TCP connection.
func (w *wsConn) Close() error {
	if w == nil || w.conn == nil {
		return nil
	}
	return w.conn.Close()
}
