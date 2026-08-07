package agent

import (
	"sync"

	"bounty/internal/provider"
)

// Session manages conversation state. The system prompt is immutable after
// construction — this keeps it cache-friendly for providers that support prompt
// caching.
type Session struct {
	mu           sync.Mutex
	SystemPrompt string
	Messages     []provider.Message
}

// NewSession creates a session seeded with a system message.
func NewSession(systemPrompt string) *Session {
	return &Session{
		SystemPrompt: systemPrompt,
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt},
		},
	}
}

// SetSystemPrompt replaces the system prompt in both the SystemPrompt field
// and the leading system message, keeping the two in sync. It is used by
// runtime model switching so the agent can identify the model it runs on.
func (s *Session) SetSystemPrompt(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SystemPrompt = prompt
	if len(s.Messages) > 0 && s.Messages[0].Role == "system" {
		s.Messages[0].Content = prompt
	}
}

// Add appends a message. Safe for concurrent use.
func (s *Session) Add(msg provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, msg)
}

// Snapshot returns a copy of the message list. Safe for concurrent use.
func (s *Session) Snapshot() []provider.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]provider.Message, len(s.Messages))
	copy(result, s.Messages)
	return result
}

// Truncate cuts the message list back to index, preserving the system message.
// If index is out of bounds the call is a no-op.
func (s *Session) Truncate(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index > 0 && index < len(s.Messages) {
		s.Messages = s.Messages[:index]
	}
}

// ReplaceMessages atomically replaces the entire message list. The caller is
// responsible for keeping a valid system message at position 0.
func (s *Session) ReplaceMessages(msgs []provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = msgs
}

// Len returns the number of messages.
func (s *Session) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Messages)
}
