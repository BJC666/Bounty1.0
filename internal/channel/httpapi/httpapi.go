package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"bounty/internal/channel"
)

// HTTPAPIChannel receives messages via HTTP POST and sends responses to a webhook URL.
type HTTPAPIChannel struct {
	id         string
	handler    channel.Handler
	active     bool
	webhookURL string
	client     *http.Client
	server     *http.Server
	port       int
}

func New(id string, handler channel.Handler, port int) *HTTPAPIChannel {
	return &HTTPAPIChannel{
		id:      id,
		handler: handler,
		port:    port,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *HTTPAPIChannel) ID() string        { return h.id }
func (h *HTTPAPIChannel) Name() string      { return "HTTP API" }
func (h *HTTPAPIChannel) IsConnected() bool { return h.active }

func (h *HTTPAPIChannel) SetWebhookURL(url string) { h.webhookURL = url }

func (h *HTTPAPIChannel) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Inbound: receive messages
	mux.HandleFunc("/api/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct {
			Text     string            `json:"text"`
			UserID   string            `json:"user_id"`
			Metadata map[string]string `json:"metadata,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		msg := channel.Message{
			ID:        fmt.Sprintf("http-%d", time.Now().UnixNano()),
			ChannelID: h.id,
			UserID:    req.UserID,
			Text:      req.Text,
			Metadata:  req.Metadata,
		}
		if err := h.OnMessage(r.Context(), msg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	})

	// Outbound status
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active": h.active,
			"id":     h.id,
		})
	})

	h.server = &http.Server{Addr: fmt.Sprintf(":%d", h.port), Handler: mux}
	h.active = true

	go func() {
		log.Printf("[httpapi] Listening on :%d", h.port)
		if err := h.server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("[httpapi] server error: %v", err)
		}
	}()

	return nil
}

func (h *HTTPAPIChannel) Stop(ctx context.Context) error {
	h.active = false
	if h.server != nil {
		return h.server.Shutdown(ctx)
	}
	return nil
}

func (h *HTTPAPIChannel) OnMessage(ctx context.Context, msg channel.Message) error {
	return h.handler.HandleMessage(ctx, msg)
}

func (h *HTTPAPIChannel) Send(ctx context.Context, reply channel.Reply, target string) error {
	// Send response to webhook URL if configured
	if h.webhookURL == "" {
		return nil
	}

	body := map[string]string{
		"text":     reply.Text,
		"markdown": reply.Markdown,
		"target":   target,
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", h.webhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook send: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

func (h *HTTPAPIChannel) HealthCheck(ctx context.Context) error {
	if !h.active {
		return fmt.Errorf("httpapi channel not active")
	}
	// Probe the status endpoint
	resp, err := h.client.Get(fmt.Sprintf("http://localhost:%d/api/status", h.port))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
