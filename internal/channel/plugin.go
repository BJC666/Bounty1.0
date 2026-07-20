package channel

import (
	"context"
	"fmt"
)

// Message represents an inbound message from a channel.
type Message struct {
	ID        string            `json:"id"`
	ChannelID string            `json:"channel_id"`
	UserID    string            `json:"user_id"`
	UserName  string            `json:"user_name"`
	Text      string            `json:"text"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	ReplyTo   string            `json:"reply_to,omitempty"`
}

// Reply represents an outbound reply to a channel.
type Reply struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown,omitempty"`
	HTML     string `json:"html,omitempty"`
}

// ChannelPlugin is the interface every messaging channel must implement.
type ChannelPlugin interface {
	// Identity
	ID() string
	Name() string

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Inbound: channel calls this when it receives a message
	OnMessage(ctx context.Context, msg Message) error

	// Outbound: called by the agent to send a reply
	Send(ctx context.Context, reply Reply, target string) error

	// Status
	IsConnected() bool
	HealthCheck(ctx context.Context) error
}

// Handler receives inbound messages from channels and routes them to the agent.
type Handler interface {
	HandleMessage(ctx context.Context, msg Message) error
}

// Registry manages active channel plugins.
type Registry struct {
	channels map[string]ChannelPlugin
	handler  Handler
}

func NewRegistry(handler Handler) *Registry {
	return &Registry{channels: make(map[string]ChannelPlugin), handler: handler}
}

func (r *Registry) Register(ch ChannelPlugin) error {
	r.channels[ch.ID()] = ch
	return nil
}

func (r *Registry) StartAll(ctx context.Context) error {
	for id, ch := range r.channels {
		if err := ch.Start(ctx); err != nil {
			return fmt.Errorf("channel %s: %w", id, err)
		}
	}
	return nil
}

func (r *Registry) StopAll(ctx context.Context) {
	for _, ch := range r.channels {
		ch.Stop(ctx)
	}
}

// Handler returns the handler that routes messages to the agent.
func (r *Registry) Handler() Handler { return r.handler }

func (r *Registry) List() []ChannelPlugin {
	var result []ChannelPlugin
	for _, ch := range r.channels {
		result = append(result, ch)
	}
	return result
}
