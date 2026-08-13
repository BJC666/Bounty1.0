package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bounty/internal/memory"
	"bounty/internal/tool"
)

// fetchClient applies a bounded timeout to all web_fetch requests.
var fetchClient = &http.Client{Timeout: 15 * time.Second}

// maxFetchBytes caps the response body read by web_fetch (1 MiB).
const maxFetchBytes = 1 * 1024 * 1024

// maxFetchRunes caps the returned text (about 32KB of UTF-8).
const maxFetchRunes = 8000

// isNonPublicHost reports whether host resolves to a loopback, private,
// link-local, unspecified, or multicast address. This blocks SSRF-style
// access to internal services from the web_fetch tool. Unresolvable
// hostnames are treated as unsafe.
func isNonPublicHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return isNonPublicIP(ip)
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return true
	}
	for _, a := range addrs {
		if isNonPublicIP(net.ParseIP(a)) {
			return true
		}
	}
	return false
}

// isNonPublicIP reports whether an IP address is loopback, private (RFC 1918
// / ULA), link-local, multicast, unspecified, or otherwise not a public
// global unicast address. Note that net.IP.IsGlobalUnicast alone is not
// sufficient: it returns true for RFC 1918 private ranges.
func isNonPublicIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		!ip.IsGlobalUnicast()
}

type WebFetchTool struct{}

func (WebFetchTool) Name() string        { return "web_fetch" }
func (WebFetchTool) ReadOnly() bool      { return true }
func (WebFetchTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "builtin"} }
func (WebFetchTool) Description() string { return "Fetches a public URL and returns its content as plain text." }
func (WebFetchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","format":"uri","maxLength":2048,"description":"The URL to fetch"}},"required":["url"],"additionalProperties":false}`)
}
func (WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct{ URL string `json:"url"` }
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	u, err := url.Parse(params.URL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme: %q", u.Scheme)
	}
	if isNonPublicHost(u.Host) {
		return "", fmt.Errorf("refusing to fetch non-public address: %s", u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Bounty/1.0")
	resp, err := fetchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	runes := []rune(string(body))
	if len(runes) > maxFetchRunes {
		runes = append(runes[:maxFetchRunes], []rune("\n... [truncated]")...)
	}
	content := string(runes)

	// Untrusted web content is wrapped in a <data> boundary so the model can
	// distinguish page content from instructions (BIPIA-style defense).
	// Suspicious pages are flagged but still returned inside the boundary —
	// marking is the defense, blocking would break legitimate fetches.
	if hits := memory.ScanAll(content); len(hits) > 0 {
		return WrapDataBoundary(u.String(), "[SECURITY] page content contains prompt-injection markers: "+strings.Join(hits, ", ")+"\n"+content), nil
	}
	return WrapDataBoundary(u.String(), content), nil
}

// WrapDataBoundary wraps untrusted content in a <data> prompt boundary.
// Literal closing tags inside the content are escaped so a hostile page
// cannot prematurely close the boundary.
func WrapDataBoundary(url, content string) string {
	content = strings.ReplaceAll(content, "</data>", "<\\/data>")
	url = strings.ReplaceAll(url, `"`, "%22")
	return "<data url=\"" + url + "\">\n" + content + "\n</data>"
}
