package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionInsights tracks per-session statistics.
type SessionInsights struct {
	mu             sync.Mutex
	SessionID      string
	StartTime      time.Time
	TotalTurns     int
	TotalTokensIn  int
	TotalTokensOut int
	ToolUseCount   map[string]int
	Errors         []string
	SkillsUsed     []string
}

// NewSessionInsights creates a new SessionInsights for the given session ID.
func NewSessionInsights(sessionID string) *SessionInsights {
	return &SessionInsights{
		SessionID:    sessionID,
		StartTime:    time.Now(),
		ToolUseCount: make(map[string]int),
	}
}

// RecordTurn records a completed turn.
func (si *SessionInsights) RecordTurn(tokensIn, tokensOut int, toolsUsed []string, skill string) {
	si.mu.Lock()
	defer si.mu.Unlock()
	si.TotalTurns++
	si.TotalTokensIn += tokensIn
	si.TotalTokensOut += tokensOut
	for _, t := range toolsUsed {
		si.ToolUseCount[t]++
	}
	if skill != "" {
		si.SkillsUsed = append(si.SkillsUsed, skill)
	}
}

// RecordError records an error.
func (si *SessionInsights) RecordError(err string) {
	si.mu.Lock()
	defer si.mu.Unlock()
	si.Errors = append(si.Errors, err)
}

// Summary returns a formatted session summary.
func (si *SessionInsights) Summary() string {
	si.mu.Lock()
	defer si.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("## Session Insights\n")
	sb.WriteString(fmt.Sprintf("- Duration: %s\n", time.Since(si.StartTime).Round(time.Second)))
	sb.WriteString(fmt.Sprintf("- Turns: %d\n", si.TotalTurns))
	sb.WriteString(fmt.Sprintf("- Tokens: %d in / %d out\n", si.TotalTokensIn, si.TotalTokensOut))

	if len(si.ToolUseCount) > 0 {
		sb.WriteString("- Top tools:\n")
		type kv struct {
			k string
			v int
		}
		var sorted []kv
		for k, v := range si.ToolUseCount {
			sorted = append(sorted, kv{k, v})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })
		for i, kv := range sorted {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("  %s: %d\n", kv.k, kv.v))
		}
	}
	if len(si.Errors) > 0 {
		sb.WriteString(fmt.Sprintf("- Errors: %d\n", len(si.Errors)))
	}
	if len(si.SkillsUsed) > 0 {
		sb.WriteString(fmt.Sprintf("- Skills used: %s\n", strings.Join(si.SkillsUsed, ", ")))
	}
	return sb.String()
}
