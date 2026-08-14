package builtin

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
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
func (WebFetchTool) Description() string { return "Fetch a public URL; returns content as plain text." }
func (WebFetchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","format":"uri","maxLength":2048},"proof":{"type":"boolean","description":"Attach session commitment for https"}},"required":["url"]}`)
}
func (WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		URL   string `json:"url"`
		Proof bool   `json:"proof"`
	}
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

	// P8-4: proof=true 时附加会话承诺（TLS 指纹 + 证书链摘要 + 时间戳 +
	// 响应体摘要）。证明放在 <data> 边界之外——它是机器可读证据而非页面内容，
	// 可原样喂给 DeVET /chain/mirror 的 web_proofs。
	var proofBlock string
	if params.Proof {
		if u.Scheme != "https" || resp.TLS == nil {
			return "", fmt.Errorf("proof mode requires an https response with TLS state")
		}
		proofJSON, err := makeSessionCommitment(u, resp.TLS, body, resp.StatusCode)
		if err != nil {
			return "", err
		}
		proofBlock = "\n\n---WEBPROOF---\n" + string(proofJSON)
	}

	// Untrusted web content is wrapped in a <data> boundary so the model can
	// distinguish page content from instructions (BIPIA-style defense).
	// Suspicious pages are flagged but still returned inside the boundary —
	// marking is the defense, blocking would break legitimate fetches.
	if hits := memory.ScanAll(content); len(hits) > 0 {
		return WrapDataBoundary(u.String(), "[SECURITY] page content contains prompt-injection markers: "+strings.Join(hits, ", ")+"\n"+content) + proofBlock, nil
	}
	return WrapDataBoundary(u.String(), content) + proofBlock, nil
}

// sessionProof is the P8-4 session commitment: 真实 HTTPS 会话的可复现承诺，
// 与 vet_webproofs/prover.py 的 attestation schema 对齐，可原样喂给 DeVET。
type sessionProof struct {
	Type            string `json:"type"`
	URL             string `json:"url"`
	Status          int    `json:"status"`
	Timestamp       string `json:"timestamp"`
	TLSVersion      string `json:"tls_version"`
	CipherSuite     string `json:"cipher_suite"`
	CertChainDigest string `json:"cert_chain_digest"`
	BodyDigest      string `json:"body_digest"`
	ServerName      string `json:"server_name"`
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS1.0"
	case tls.VersionTLS11:
		return "TLS1.1"
	case tls.VersionTLS12:
		return "TLS1.2"
	case tls.VersionTLS13:
		return "TLS1.3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

// makeSessionCommitment 从一次真实 HTTPS 响应捕获会话承诺：
// cert_chain_digest = sha256(逐证书 DER 拼接)；body_digest = sha256(响应体)；
// 时间戳 RFC3339 UTC；TLS 版本/密码套件来自连接状态。
func makeSessionCommitment(u *url.URL, state *tls.ConnectionState, body []byte, status int) ([]byte, error) {
	if state == nil {
		return nil, fmt.Errorf("no TLS state available (non-https response?)")
	}
	chainHash := sha256.New()
	for _, c := range state.PeerCertificates {
		chainHash.Write(c.Raw)
	}
	bodySum := sha256.Sum256(body)
	p := sessionProof{
		Type:            "session_commitment",
		URL:             u.String(),
		Status:          status,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		TLSVersion:      tlsVersionName(state.Version),
		CipherSuite:     tls.CipherSuiteName(state.CipherSuite),
		CertChainDigest: hex.EncodeToString(chainHash.Sum(nil)),
		BodyDigest:      hex.EncodeToString(bodySum[:]),
		ServerName:      u.Hostname(),
	}
	return json.Marshal(p)
}

// WrapDataBoundary wraps untrusted content in a <data> prompt boundary.
// Literal closing tags inside the content are escaped so a hostile page
// cannot prematurely close the boundary.
func WrapDataBoundary(url, content string) string {
	content = strings.ReplaceAll(content, "</data>", "<\\/data>")
	url = strings.ReplaceAll(url, `"`, "%22")
	return "<data url=\"" + url + "\">\n" + content + "\n</data>"
}
