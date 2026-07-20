package webhook

import (
	"context"
	"fmt"
	"log"

	"bounty/internal/channel"
)

// WebhookChannel is a simple HTTP webhook channel.
// External services POST messages to the gateway's /webhook/<id> endpoint.
type WebhookChannel struct {
	id      string
	name    string
	handler channel.Handler
	active  bool
}

func New(id, name string, handler channel.Handler) *WebhookChannel {
	return &WebhookChannel{id: id, name: name, handler: handler}
}

func (w *WebhookChannel) ID() string        { return w.id }
func (w *WebhookChannel) Name() string      { return w.name }
func (w *WebhookChannel) IsConnected() bool { return w.active }

func (w *WebhookChannel) Start(ctx context.Context) error {
	w.active = true
	log.Printf("Webhook channel %q ready — POST to /webhook/%s", w.name, w.id)
	return nil
}

func (w *WebhookChannel) Stop(ctx context.Context) error {
	w.active = false
	return nil
}

func (w *WebhookChannel) HealthCheck(ctx context.Context) error {
	if !w.active {
		return fmt.Errorf("channel not active")
	}
	return nil
}

func (w *WebhookChannel) OnMessage(ctx context.Context, msg channel.Message) error {
	return w.handler.HandleMessage(ctx, msg)
}

func (w *WebhookChannel) Send(ctx context.Context, reply channel.Reply, target string) error {
	// Webhook channels typically don't send outbound — replies go to the webhook caller
	log.Printf("Webhook %q reply to %s: %s", w.name, target, reply.Text)
	return nil
}
