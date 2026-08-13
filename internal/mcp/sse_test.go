package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"bounty/internal/tool"
)

// fakeSSEServer is a minimal MCP-over-SSE server: GET /sse announces the POST
// endpoint and streams responses; POST /message answers the JSON-RPC request
// over the SSE stream.
type fakeSSEServer struct {
	mu  sync.Mutex
	fl  http.Flusher
	w   http.ResponseWriter
	url string
}

func newFakeSSEServer() *fakeSSEServer {
	f := &fakeSSEServer{}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	f.url = srv.URL
	return f
}

func (f *fakeSSEServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/sse":
		w.Header().Set("Content-Type", "text/event-stream")
		f.mu.Lock()
		f.w, f.fl = w, w.(http.Flusher)
		f.mu.Unlock()
		fmt.Fprint(w, "event: endpoint\ndata: /message\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	case r.Method == http.MethodPost && r.URL.Path == "/message":
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		result := fakeResult(req)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}
		data, _ := json.Marshal(resp)
		f.mu.Lock()
		if f.w != nil {
			fmt.Fprintf(f.w, "event: message\ndata: %s\n\n", data)
			f.fl.Flush()
		}
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func fakeResult(req struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}) any {
	switch req.Method {
	case "initialize":
		return map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "ssefake", "version": "1.0"}}
	case "tools/list":
		return map[string]any{"tools": []any{map[string]any{
			"name": "add", "description": "加法",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "number"},
					"b": map[string]any{"type": "number"},
				},
				"required": []string{"a", "b"},
			},
		}}}
	case "tools/call":
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "sum-ok"}}}
	case "resources/list":
		return map[string]any{"resources": []any{}}
	case "prompts/list":
		return map[string]any{"prompts": []any{}}
	default:
		return map[string]any{}
	}
}

func TestSSETransportEndToEnd(t *testing.T) {
	srv := newFakeSSEServer()

	h := NewHost()
	t.Cleanup(h.Close)
	if err := h.Connect(Spec{Name: "ssefake", URL: srv.url, Timeout: 30}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	reg := tool.NewRegistry()
	h.RegisterTools(reg)
	got, ok := reg.Get("mcp__ssefake__add")
	if !ok {
		t.Fatal("SSE tool not registered")
	}
	out, err := got.Execute(context.Background(), json.RawMessage(`{"a":1,"b":2}`))
	if err != nil || !strings.Contains(out, "sum-ok") {
		t.Fatalf("SSE tool call: out=%q err=%v", out, err)
	}
}

func TestSSETransportBadEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	h := NewHost()
	if err := h.Connect(Spec{Name: "bad", URL: srv.URL, Timeout: 5}); err == nil {
		t.Fatal("expected connect failure")
	}
}
