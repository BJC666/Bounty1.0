package agent

import "bounty/internal/provider"

// maybeCompact triggers compaction when the message list exceeds 200 entries.
func (a *Agent) maybeCompact(sess *Session) {
	if sess.Len() > 200 {
		a.compact(sess)
	}
}

// compact keeps the system message, injects a summary placeholder, and
// preserves the last 20 messages so recent context is retained.
func (a *Agent) compact(sess *Session) {
	msgs := sess.Snapshot()
	if len(msgs) <= 22 {
		return
	}
	// Keep the system message at [0], add a summary marker, then the tail.
	tail := msgs[len(msgs)-20:]
	newMsgs := make([]provider.Message, 1, 22)
	newMsgs[0] = msgs[0]
	newMsgs = append(newMsgs, provider.Message{
		Role:    "user",
		Content: "[Earlier conversation has been summarized to conserve context.]",
	})
	newMsgs = append(newMsgs, tail...)
	sess.ReplaceMessages(newMsgs)
}
