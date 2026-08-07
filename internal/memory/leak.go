package memory

import "regexp"

// SensitivePatterns detect secrets that must never leak into live output
// streams (consoles, SSE clients): API keys, private keys, credentials.
// Inspired by the CoT-leakage finding that reasoning traces can contain
// more actionable secret material than the final answer.
var sensitivePatterns = []*regexp.Regexp{
	// OpenAI / DeepSeek / Anthropic style keys: sk-...
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),
	// AWS access key IDs
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// GitHub personal access tokens
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	// Slack tokens (xoxb- / xoxp- / xoxa- / xoxr- / xoxs-)
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`),
	// PEM private keys
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`),
	// password / api_key / access_token / secret assignments
	regexp.MustCompile(`(?i)\bpassword\s*[:=]\s*[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|secret)\s*[:=]\s*[^\s,;]{8,}`),
}

// ScanSensitive returns the secret-looking substrings found in content.
func ScanSensitive(content string) []string {
	var hits []string
	for _, re := range sensitivePatterns {
		hits = append(hits, re.FindAllString(content, -1)...)
	}
	return hits
}

// RedactSensitive replaces secret-looking substrings with "[redacted]".
func RedactSensitive(content string) string {
	if content == "" {
		return content
	}
	for _, re := range sensitivePatterns {
		content = re.ReplaceAllString(content, "[redacted]")
	}
	return content
}
