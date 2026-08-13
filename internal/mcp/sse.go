package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// sseTransport implements the MCP HTTP+SSE transport:
//   - GET <base>/sse opens the event stream; the server announces its POST
//     endpoint in an `event: endpoint` message.
//   - JSON-RPC requests are POSTed to that endpoint; responses arrive back
//     over the SSE stream as `event: message` payloads, matched by ID.
type sseTransport struct {
	mu       sync.Mutex
	endpoint *url.URL
	pending  map[int]chan jsonrpcResponse
	reqID    int
	client   *http.Client
	cancel   context.CancelFunc
}

func newSSETransport(ctx context.Context, spec Spec) (*sseTransport, error) {
	base, err := url.Parse(spec.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.URL, err)
	}
	sseURL := *base
	if !strings.HasSuffix(sseURL.Path, "/sse") {
		sseURL.Path = strings.TrimSuffix(sseURL.Path, "/") + "/sse"
	}

	timeout := time.Duration(spec.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	streamCtx, cancel := context.WithCancel(context.Background())

	t := &sseTransport{
		pending: make(map[int]chan jsonrpcResponse),
		client:  client,
		cancel:  cancel,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL.String(), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open SSE stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("open SSE stream: status %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	// The endpoint event must arrive before any request can be sent.
	endpoint, err := readEndpointEvent(reader, timeout)
	if err != nil {
		resp.Body.Close()
		cancel()
		return nil, err
	}
	resolved := base.ResolveReference(endpoint)
	t.endpoint = resolved

	go t.readLoop(streamCtx, reader, resp.Body)
	return t, nil
}

// readEndpointEvent consumes SSE events until the `endpoint` event arrives.
func readEndpointEvent(r *bufio.Reader, timeout time.Duration) (*url.URL, error) {
	deadline := time.Now().Add(timeout)
	for {
		event, data, err := readSSEEvent(r)
		if err != nil {
			return nil, fmt.Errorf("read endpoint event: %w", err)
		}
		if event == "endpoint" && strings.TrimSpace(data) != "" {
			u, err := url.Parse(strings.TrimSpace(data))
			if err != nil {
				return nil, fmt.Errorf("parse endpoint %q: %w", data, err)
			}
			return u, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("endpoint event not received within %s", timeout)
		}
	}
}

// readSSEEvent reads one SSE event block: lines of `field: value`, terminated
// by a blank line. Returns the accumulated event and data fields.
func readSSEEvent(r *bufio.Reader) (string, string, error) {
	var event, data string
	for {
		line, err := r.ReadString('\n')
		if err != nil && len(line) == 0 {
			return "", "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return event, data, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if data == "" {
				data = value
			} else {
				data += "\n" + value
			}
		}
	}
}

func (t *sseTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	t.reqID++
	reqID := t.reqID
	ch := make(chan jsonrpcResponse, 1)
	t.pending[reqID] = ch
	req := jsonrpcRequest{JSONRPC: "2.0", ID: reqID, Method: method, Params: params}
	t.mu.Unlock()

	if err := t.post(ctx, req); err != nil {
		t.mu.Lock()
		delete(t.pending, reqID)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, reqID)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("MCP SSE transport closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (t *sseTransport) post(ctx context.Context, req jsonrpcRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("SSE post status %d", resp.StatusCode)
	}
	return nil
}

func (t *sseTransport) notify(ctx context.Context, method string, params any) error {
	return t.post(ctx, jsonrpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (t *sseTransport) close() {
	t.cancel()
}

// readLoop routes SSE `message` events to the pending request channel keyed
// by JSON-RPC ID; anything else is ignored.
func (t *sseTransport) readLoop(ctx context.Context, r *bufio.Reader, closer io.Closer) {
	defer closer.Close()
	for {
		if ctx.Err() != nil {
			t.failAll()
			return
		}
		event, data, err := readSSEEvent(r)
		if err != nil {
			t.failAll()
			return
		}
		if event != "message" {
			continue
		}
		var resp jsonrpcResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			continue
		}
		t.mu.Lock()
		ch, ok := t.pending[resp.ID]
		if ok {
			delete(t.pending, resp.ID)
		}
		t.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

func (t *sseTransport) failAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, ch := range t.pending {
		delete(t.pending, id)
		close(ch)
	}
}
