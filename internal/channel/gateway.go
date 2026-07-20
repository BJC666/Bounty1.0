package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"bounty/internal/control"
	"bounty/internal/event"
)

// Gateway is an HTTP server that bridges channels and the agent controller.
type Gateway struct {
	ctrl     *control.Controller
	registry *Registry
	server   *http.Server
	port     int
	mu       sync.Mutex
	sessions map[string]string // channel_user → sessionID
}

func NewGateway(ctrl *control.Controller, registry *Registry, port int) *Gateway {
	return &Gateway{
		ctrl:     ctrl,
		registry: registry,
		port:     port,
		sessions: make(map[string]string),
	}
}

func (g *Gateway) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Channel list
	mux.HandleFunc("/channels", func(w http.ResponseWriter, r *http.Request) {
		channels := g.registry.List()
		var names []string
		for _, ch := range channels {
			names = append(names, ch.ID())
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"channels": names,
		})
	})

	// Webhook receive endpoint — channels POST messages here
	mux.HandleFunc("/webhook/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		channelID := r.URL.Path[len("/webhook/"):]
		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		msg.ChannelID = channelID

		// Route to agent
		if err := g.registry.handler.HandleMessage(r.Context(), msg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(202)
		w.Write([]byte(`{"status":"accepted"}`))
	})

	// SSE event stream — frontends subscribe here
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}

		sink := &sseSink{w: w, flusher: flusher}
		// Note: in a full implementation, the sink would be registered
		// with the controller and receive events from agent turns
		_ = sink

		<-r.Context().Done()
	})

	// Chat API — direct message to agent
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := g.ctrl.Send(r.Context(), req.Message); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(202)
		w.Write([]byte(`{"status":"accepted"}`))
	})

	g.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", g.port),
		Handler: mux,
	}

	// Start channels
	if err := g.registry.StartAll(ctx); err != nil {
		return fmt.Errorf("start channels: %w", err)
	}

	log.Printf("Gateway listening on :%d", g.port)
	return g.server.ListenAndServe()
}

func (g *Gateway) Stop(ctx context.Context) {
	g.registry.StopAll(ctx)
	if g.server != nil {
		g.server.Shutdown(ctx)
	}
}

// sseSink implements event.Sink for Server-Sent Events.
type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func (s *sseSink) Emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(ev)
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}
