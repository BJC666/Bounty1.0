package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bounty/internal/provider"
)

// ReviewConfig configures background review behavior.
type ReviewConfig struct {
	Enabled  bool
	MaxWait  time.Duration // max time for review
	MinTurns int           // minimum turns before first review
}

// BackgroundReviewer runs post-turn reflection to save skills/memories.
type BackgroundReviewer struct {
	cfg      ReviewConfig
	mu       sync.Mutex
	lastTurn int
}

// NewBackgroundReviewer creates a BackgroundReviewer with the given config.
func NewBackgroundReviewer(cfg ReviewConfig) *BackgroundReviewer {
	if cfg.MaxWait == 0 {
		cfg.MaxWait = 10 * time.Second
	}
	return &BackgroundReviewer{cfg: cfg}
}

// ShouldRun returns true if a review should run after this turn.
func (br *BackgroundReviewer) ShouldRun(turnCount int) bool {
	if !br.cfg.Enabled {
		return false
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	if turnCount < br.cfg.MinTurns {
		return false
	}
	if turnCount-br.lastTurn < 3 {
		return false
	} // every 3 turns
	br.lastTurn = turnCount
	return true
}

// RunReview spawns a lightweight reflection on the conversation.
// Returns suggested memory/skill updates (if any).
// memoryDir is reserved for future use (persisting extracted memories to disk).
func (br *BackgroundReviewer) RunReview(ctx context.Context, messages []provider.Message, prov provider.Provider, memoryDir string) *ReviewResult {
	_ = memoryDir // reserved for future use

	ctx, cancel := context.WithTimeout(ctx, br.cfg.MaxWait)
	defer cancel()

	// Build a compact review prompt
	var convBuilder strings.Builder
	convBuilder.WriteString("Review this conversation and suggest:\n")
	convBuilder.WriteString("1. Any facts worth saving to project memory\n")
	convBuilder.WriteString("2. Any reusable skills that could be extracted\n")
	convBuilder.WriteString("\nConversation:\n")

	// Only include last 10 user/assistant exchanges
	count := 0
	for i := len(messages) - 1; i >= 0 && count < 10; i-- {
		m := messages[i]
		if m.Role == "user" || m.Role == "assistant" {
			content := m.Content
			if runes := []rune(content); len(runes) > 500 {
				content = string(runes[:500]) + "..."
			}
			convBuilder.WriteString(fmt.Sprintf("[%s] %s\n", m.Role, content))
			count++
		}
	}

	ch, err := prov.Stream(ctx, []provider.Message{
		{Role: "user", Content: convBuilder.String()},
	}, nil, provider.StreamOpts{Temperature: 0})

	if err != nil {
		return nil
	}

	var result strings.Builder
	for ev := range ch {
		if ev.Delta != nil && ev.Delta.Content != "" {
			result.WriteString(ev.Delta.Content)
		}
		if ev.Done {
			break
		}
	}

	return &ReviewResult{
		Suggestion: result.String(),
		Timestamp:  time.Now(),
	}
}

// ReviewResult holds the output of a background review.
type ReviewResult struct {
	Suggestion string
	Timestamp  time.Time
}

// SkillNudge generates a prompt to encourage skill creation.
func SkillNudge(turnsSinceLastSkill int) string {
	if turnsSinceLastSkill < 5 {
		return ""
	}
	return "💡 You've completed several complex tasks. Consider whether anything you learned should become a reusable skill."
}
