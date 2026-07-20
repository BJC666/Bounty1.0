package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"bounty/internal/tool"
)

// Transport abstracts the connection to an MCP server.
type transport interface {
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close()
}

// Spec describes an MCP server to connect to.
type Spec struct {
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"` // stdio
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	URL     string   `json:"url,omitempty"` // HTTP
	Timeout int      `json:"timeout"`       // seconds, default 300
}

// Client wraps one MCP server connection.
type Client struct {
	spec  Spec
	trans transport
	tools []*mcpTool
	mu    sync.Mutex
}

// Host manages multiple MCP connections.
type Host struct {
	clients map[string]*Client
	mu      sync.Mutex
}

// NewHost creates a new Host.
func NewHost() *Host { return &Host{clients: make(map[string]*Client)} }

// Connect starts an MCP server and discovers its tools.
func (h *Host) Connect(spec Spec) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.clients[spec.Name]; exists {
		return fmt.Errorf("server %q already connected", spec.Name)
	}

	var trans transport
	var err error

	if spec.Command != "" {
		trans, err = newStdioTransport(spec)
	} else if spec.URL != "" {
		return fmt.Errorf("HTTP transport not yet implemented — use stdio")
	} else {
		return fmt.Errorf("spec must have command or url")
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", spec.Name, err)
	}

	client := &Client{spec: spec, trans: trans}
	if err := client.handshake(context.Background()); err != nil {
		trans.close()
		return fmt.Errorf("handshake %s: %w", spec.Name, err)
	}

	if err := client.discoverTools(context.Background()); err != nil {
		trans.close()
		return fmt.Errorf("discover tools %s: %w", spec.Name, err)
	}

	h.clients[spec.Name] = client
	return nil
}

// RegisterTools adds all MCP tools to the given registry.
func (h *Host) RegisterTools(reg *tool.Registry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.clients {
		c.mu.Lock()
		for _, t := range c.tools {
			reg.Add(t)
		}
		c.mu.Unlock()
	}
}

// Close disconnects all servers.
func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.clients {
		c.trans.close()
	}
}

// ── Client methods ──

func (c *Client) handshake(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name": "bounty", "version": "1.0.0",
		},
	}
	_, err := c.trans.call(ctx, "initialize", params)
	if err != nil {
		return err
	}
	return c.trans.notify(ctx, "notifications/initialized", nil)
}

func (c *Client) discoverTools(ctx context.Context) error {
	result, err := c.trans.call(ctx, "tools/list", nil)
	if err != nil {
		return err
	}

	var list struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range list.Tools {
		c.tools = append(c.tools, &mcpTool{
			client:       c,
			serverTool:   t.Name,
			name:         "mcp__" + c.spec.Name + "__" + t.Name,
			desc:         t.Description,
			schema:       t.InputSchema,
		})
	}
	return nil
}

// ── mcpTool wraps an MCP tool ──

type mcpTool struct {
	client     *Client
	serverTool string // original name on the MCP server
	name       string // namespaced name: mcp__<server>__<tool>
	desc       string
	schema     json.RawMessage
}

func (t *mcpTool) Name() string            { return t.name }
func (t *mcpTool) Description() string     { return t.desc }
func (t *mcpTool) Schema() json.RawMessage { return t.schema }
func (t *mcpTool) ReadOnly() bool          { return false }
func (t *mcpTool) Owner() tool.Owner       { return tool.Owner{Kind: "mcp", ID: t.client.spec.Name} }

func (t *mcpTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	params := map[string]any{
		"name":      t.serverTool,
		"arguments": json.RawMessage(args),
	}
	result, err := t.client.trans.call(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("mcp tool call: %w", err)
	}

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}

	var output string
	for _, c := range resp.Content {
		if c.Type == "text" {
			output += c.Text
		}
	}
	if resp.IsError {
		return output, fmt.Errorf("mcp tool error: %s", output)
	}
	return output, nil
}

// ── stdio transport ──

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *json.Decoder
	mu     sync.Mutex
	reqID  int
	kill   *time.Timer
}

func newStdioTransport(spec Spec) (*stdioTransport, error) {
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = 300
	}

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Env = append(cmd.Env, spec.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	t := &stdioTransport{
		cmd:    cmd,
		stdin:  json.NewEncoder(stdin),
		stdout: json.NewDecoder(stdout),
	}

	// Kill the process if it doesn't complete the handshake in time.
	t.kill = time.AfterFunc(time.Duration(timeout)*time.Second, func() {
		cmd.Process.Kill()
	})

	return t, nil
}

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (t *stdioTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Cancel the startup kill timer once a successful call happens.
	if t.kill != nil {
		t.kill.Stop()
		t.kill = nil
	}

	t.reqID++
	req := jsonrpcRequest{JSONRPC: "2.0", ID: t.reqID, Method: method, Params: params}
	if err := t.stdin.Encode(req); err != nil {
		return nil, err
	}

	// Read response with context support via a goroutine.
	type result struct {
		resp jsonrpcResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var resp jsonrpcResponse
		err := t.stdout.Decode(&resp)
		ch <- result{resp, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		if r.resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", r.resp.Error.Code, r.resp.Error.Message)
		}
		return r.resp.Result, nil
	}
}

func (t *stdioTransport) notify(ctx context.Context, method string, params any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	req := jsonrpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	return t.stdin.Encode(req)
}

func (t *stdioTransport) close() {
	if t.kill != nil {
		t.kill.Stop()
		t.kill = nil
	}
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}
}
