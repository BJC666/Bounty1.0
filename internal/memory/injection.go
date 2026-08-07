package memory

import "strings"

// InjectionPatterns are known prompt injection markers.
var InjectionPatterns = []string{
	"<system>", "</system>",
	"<instruction>", "</instruction>",
	"ignore previous", "ignore all previous",
	"ignore all instructions", "ignore previous instructions",
	"disregard", "override",
	"system prompt:", "system: you are now",
	"system instruction:", "system instructions:",
	"you are now", "from now on",
	"[system]", "[/system]",
	"<|im_start|>", "<|im_end|>",
	"<function_call>", "</function_call>",
	"DAN mode", "developer mode",
	"pretend you are", "act as if you are",
	// Task-aligned injection (Mind the Web style): instructions disguised as
	// helpful task guidance.
	"to complete this task,", "as part of this task,",
	"before you proceed,", "to proceed, you must",
}

// SelfReplicationPatterns are markers of self-propagating ("worm") prompts
// that instruct the model to copy/forward themselves into new contexts
// (RAGworm / DonkeyRail style).
var SelfReplicationPatterns = []string{
	"copy and paste this", "copy this message",
	"forward this message", "forward the following",
	"send this message to", "send the following to",
	"repeat this message", "repeat the following text",
	"share this with", "pass this on",
	"reproduce the following", "propagate this",
	"add this to your memory", "save this prompt",
	"include this in your next",
}

// ScanInjection checks content for prompt injection patterns.
// Returns a list of detected patterns (empty = clean).
func ScanInjection(content string) []string {
	lower := strings.ToLower(content)
	var detected []string
	for _, pattern := range InjectionPatterns {
		if strings.Contains(lower, pattern) {
			detected = append(detected, pattern)
		}
	}
	return detected
}

// ScanSelfReplication checks content for self-propagating prompt markers.
func ScanSelfReplication(content string) []string {
	lower := strings.ToLower(content)
	var detected []string
	for _, pattern := range SelfReplicationPatterns {
		if strings.Contains(lower, pattern) {
			detected = append(detected, pattern)
		}
	}
	return detected
}

// ScanAll reports both classic injection markers and self-replication markers.
func ScanAll(content string) []string {
	detected := ScanInjection(content)
	return append(detected, ScanSelfReplication(content)...)
}

// IsSafe returns true if content passes the injection scan.
func IsSafe(content string) bool {
	return len(ScanInjection(content)) == 0
}

// IsSafeAll returns true if content passes both the injection and the
// self-replication scan.
func IsSafeAll(content string) bool {
	return len(ScanAll(content)) == 0
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
