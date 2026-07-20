package memory

import "strings"

// InjectionPatterns are known prompt injection markers.
var InjectionPatterns = []string{
	"<system>", "</system>",
	"<instruction>", "</instruction>",
	"ignore previous", "ignore all previous",
	"system prompt:", "system: you are now",
	"[system]", "[/system]",
	"<|im_start|>", "<|im_end|>",
	"<function_call>", "</function_call>",
	"DAN mode", "developer mode",
	"pretend you are", "act as if you are",
}

// ScanInjection checks content for prompt injection patterns.
// Returns a list of detected patterns (empty = clean).
func ScanInjection(content string) []string {
	lower := strings.ToLower(content)
	var detected []string
	for _, pattern := range InjectionPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			detected = append(detected, pattern)
		}
	}
	return detected
}

// IsSafe returns true if content passes injection scan.
func IsSafe(content string) bool {
	return len(ScanInjection(content)) == 0
}

// Sanitize removes or replaces injection patterns from content.
func Sanitize(content string) string {
	result := content
	for _, pattern := range InjectionPatterns {
		result = strings.ReplaceAll(result, pattern, "[filtered]")
		result = strings.ReplaceAll(result, strings.ToLower(pattern), "[filtered]")
	}
	return result
}
