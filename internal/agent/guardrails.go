package agent

import "bounty/internal/provider"

// checkGuardrails tracks error patterns (storm signals) and blocked-turn
// streaks so the agent can detect and react to degenerate loops.
func (a *Agent) checkGuardrails(toolCalls []provider.ToolCall, results []toolResult) {
	for _, tr := range results {
		if tr.Err != nil {
			key := tr.Name + ":" + tr.Err.Error()
			a.stormSig[key]++
		}
	}
	allFailed := true
	for _, tr := range results {
		if tr.Err == nil {
			allFailed = false
			break
		}
	}
	if allFailed && len(results) > 0 {
		a.blockedTurnStreak++
	} else {
		a.blockedTurnStreak = 0
	}
}
