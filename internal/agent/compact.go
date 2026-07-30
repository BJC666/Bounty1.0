package agent

import (
	"fmt"

	"bounty/internal/provider"
)

const (
	charsPerToken     = 4
	defaultTailTokens = 16384
	minTailMessages   = 2
)

// CompactConfig holds compaction thresholds.
type CompactConfig struct {
	SoftRatio  float64
	Ratio      float64
	ForceRatio float64
	TailTokens int
	MaxContext int
}

// estimateTokens provides a rough token count (4 chars approx 1 token).
func estimateTokens(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / charsPerToken
		total += len(m.ToolName) / charsPerToken
		for _, tc := range m.ToolCalls {
			total += len(tc.Name) / charsPerToken
			total += len(tc.Args) / charsPerToken
		}
	}
	if total == 0 {
		total = 1
	}
	return total
}

func (a *Agent) maybeCompact(sess *Session) {
	cfg := CompactConfig{
		SoftRatio:  0.5,
		Ratio:       0.8,
		ForceRatio:  0.9,
		TailTokens:  defaultTailTokens,
		MaxContext:  200000,
	}
	a.compactWithConfig(sess, cfg)
}

func (a *Agent) compactWithConfig(sess *Session, cfg CompactConfig) {
	msgs := sess.Snapshot()
	if len(msgs) <= 2 {
		return
	}

	tokens := estimateTokens(msgs)
	threshold := int(float64(cfg.MaxContext) * cfg.Ratio)
	forceThreshold := int(float64(cfg.MaxContext) * cfg.ForceRatio)

	if tokens < threshold {
		return
	}

	// Force compact: keep only system + last 2 messages.
	if tokens >= forceThreshold {
		tail := tailMessages(msgs, cfg.TailTokens, 2)
		newMsgs := make([]provider.Message, 0, len(tail)+2)
		newMsgs = append(newMsgs, msgs[0]) // system
		newMsgs = append(newMsgs, provider.Message{
			Role:    "user",
			Content: "[Earlier conversation was dropped due to context length limits. Key information from earlier turns may be lost.]",
		})
		newMsgs = append(newMsgs, tail...)
		sess.ReplaceMessages(newMsgs)
		return
	}

	// Normal compact: keep tail within budget.
	tail := tailMessages(msgs, cfg.TailTokens, minTailMessages)
	if len(tail) >= len(msgs)-1 {
		return // nothing to compact
	}

	newMsgs := make([]provider.Message, 0, len(tail)+2)
	newMsgs = append(newMsgs, msgs[0]) // system
	newMsgs = append(newMsgs, provider.Message{
		Role:    "user",
		Content: "[Earlier conversation has been summarized to conserve context.]",
	})
	newMsgs = append(newMsgs, tail...)
	sess.ReplaceMessages(newMsgs)
}

// tailMessages returns the last messages that fit within tokenBudget, keeping at
// least minMessages (excluding the system prompt at index 0).
func tailMessages(msgs []provider.Message, tokenBudget int, minMessages int) []provider.Message {
	if len(msgs) <= minMessages+1 {
		return msgs[1:] // exclude system prompt
	}

	tail := make([]provider.Message, 0)
	tokens := 0
	for i := len(msgs) - 1; i >= 1; i-- { // skip system prompt at index 0
		msgTokens := estimateTokens([]provider.Message{msgs[i]})
		if tokens+msgTokens > tokenBudget && len(tail) >= minMessages {
			break
		}
		tail = append([]provider.Message{msgs[i]}, tail...)
		tokens += msgTokens
	}
	return tail
}

// SoftCompactNotice returns a warning if context is above the soft threshold.
func SoftCompactNotice(tokens int, maxContext int, softRatio float64) string {
	threshold := int(float64(maxContext) * softRatio)
	if tokens > threshold {
		used := float64(tokens) / float64(maxContext) * 100
		return fmt.Sprintf("Context is %.0f%% full (%d/%d tokens). Consider starting a new session or compacting.",
			used, tokens, maxContext)
	}
	return ""
}
