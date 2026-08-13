package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"bounty/internal/memory"
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

// RunReview spawns a lightweight reflection on the conversation. The model
// is asked for durable facts as a JSON array; safe entries are persisted to
// <memoryDir>/.agent/memory (project auto-memory) so they survive into later
// sessions. Entries carrying injection/self-replication markers are rejected.
func (br *BackgroundReviewer) RunReview(ctx context.Context, messages []provider.Message, prov provider.Provider, memoryDir string) *ReviewResult {
	ctx, cancel := context.WithTimeout(ctx, br.cfg.MaxWait)
	defer cancel()

	var convBuilder strings.Builder
	convBuilder.WriteString("Review this conversation and extract durable facts worth persisting to project auto-memory (user preferences, conventions, decisions, lessons learned).\n")
	convBuilder.WriteString("Respond with ONLY a JSON array of objects with keys \"name\" (short kebab-case), \"description\" (one line), \"content\" (the fact itself). If nothing is worth saving, respond with []. Do not include any other text.\n\n")
	convBuilder.WriteString("Conversation:\n")

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

	var resultText strings.Builder
	for ev := range ch {
		if ev.Err != nil {
			return nil
		}
		if ev.Delta != nil && ev.Delta.Content != "" {
			resultText.WriteString(ev.Delta.Content)
		}
		if ev.Done {
			break
		}
	}

	res := &ReviewResult{
		Suggestion: resultText.String(),
		Timestamp:  time.Now(),
	}
	if memoryDir != "" {
		res.Saved, res.Rejected = persistSuggestions(memoryDir, res.Suggestion)
	}
	return res
}

// memorySuggestion is the structured shape the reviewer is asked to emit.
type memorySuggestion struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

// extractSuggestions parses the reviewer output leniently: it accepts a JSON
// array, a bare JSON object, or text with an embedded JSON block.
func extractSuggestions(text string) []memorySuggestion {
	trimmed := strings.TrimSpace(text)
	var payload string
	switch {
	case strings.HasPrefix(trimmed, "["):
		if end := strings.LastIndex(trimmed, "]"); end >= 0 {
			payload = trimmed[:end+1]
		}
	case strings.HasPrefix(trimmed, "{"):
		if end := strings.LastIndex(trimmed, "}"); end >= 0 {
			payload = "[" + trimmed[:end+1] + "]"
		}
	}
	if payload == "" {
		return nil
	}
	var items []memorySuggestion
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		return nil
	}
	return items
}

// persistSuggestions saves safe suggestions to the project memory store and
// reports the entries it rejected (injection / self-replication markers).
func persistSuggestions(memoryDir string, text string) (saved []memory.Entry, rejected []string) {
	items := extractSuggestions(text)
	for _, it := range items {
		name := strings.TrimSpace(it.Name)
		desc := strings.TrimSpace(it.Description)
		content := strings.TrimSpace(it.Content)
		if content == "" {
			continue
		}
		if !memory.IsSafeAll(name + " " + desc + " " + content) {
			label := name
			if label == "" {
				label = desc
			}
			if label == "" {
				label = truncateForLabel(content)
			}
			rejected = append(rejected, label)
			continue
		}
		if name == "" {
			sum := sha256.Sum256([]byte(desc + content))
			name = fmt.Sprintf("auto-%x", sum[:4])
		}
		if desc == "" {
			desc = name
		}
		if err := memory.NewRememberStore(memoryDir).Save(name, desc, content); err != nil {
			continue
		}
		saved = append(saved, memory.Entry{Name: name, Description: desc, Content: content, CreatedAt: time.Now()})
	}
	return saved, rejected
}

func truncateForLabel(s string) string {
	r := []rune(s)
	if len(r) <= 40 {
		return s
	}
	return string(r[:40]) + "..."
}

// ReviewResult holds the output of a background review.
type ReviewResult struct {
	Suggestion string
	Timestamp  time.Time
	// Saved lists entries persisted to project auto-memory.
	Saved []memory.Entry
	// Rejected lists suggested entries refused because they carried
	// injection / self-replication markers.
	Rejected []string
}

// SkillNudge generates a prompt to encourage skill creation.
func SkillNudge(turnsSinceLastSkill int) string {
	if turnsSinceLastSkill < 5 {
		return ""
	}
	return "💡 You've completed several complex tasks. Consider whether anything you learned should become a reusable skill."
}
