package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	URL     string   `json:"url,omitempty"` // HTTP+SSE
	Timeout int      `json:"timeout"`       // seconds, default 300
	// ReadOnly marks every tool exposed by this server as read-only
	// (server-level permission annotation). Trusted servers may forward
	// their own per-tool readOnly hints instead.
	ReadOnly bool `json:"read_only,omitempty"`
	Trust    bool `json:"trust,omitempty"`
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
		trans, err = newSSETransport(context.Background(), spec)
	} else {
		return fmt.Errorf("spec must have command or url")
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", spec.Name, err)
	}

	client := &Client{spec: spec, trans: trans}
	connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.handshake(connectCtx); err != nil {
		trans.close()
		return fmt.Errorf("handshake %s: %w", spec.Name, err)
	}

	if err := client.discoverTools(connectCtx); err != nil {
		trans.close()
		return fmt.Errorf("discover tools %s: %w", spec.Name, err)
	}
	// Resources and prompts are best-effort: servers without them just skip.
	if err := client.discoverResources(connectCtx); err != nil {
		trans.close()
		return fmt.Errorf("discover resources %s: %w", spec.Name, err)
	}
	if err := client.discoverPrompts(connectCtx); err != nil {
		trans.close()
		return fmt.Errorf("discover prompts %s: %w", spec.Name, err)
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
	h.clients = make(map[string]*Client)
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
			client:     c,
			serverTool: t.Name,
			name:       "mcp__" + c.spec.Name + "__" + t.Name,
			desc:       t.Description,
			schema:     t.InputSchema,
			readOnly:   c.spec.ReadOnly,
		})
	}
	return nil
}

// discoverResources registers every MCP resource as a read-only tool
// (mcp__<server>__resource_<n>) that reads a fixed URI.
func (c *Client) discoverResources(ctx context.Context) error {
	result, err := c.trans.call(ctx, "resources/list", nil)
	if err != nil {
		// resources are optional; missing capability errors are not fatal
		return nil
	}
	var list struct {
		Resources []struct {
			URI         string `json:"uri"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, r := range list.Resources {
		uri := r.URI
		schema := json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
		c.tools = append(c.tools, &mcpTool{
			client:     c,
			serverTool: "resource_" + fmt.Sprintf("%d", i+1),
			name:       fmt.Sprintf("mcp__%s__resource_%d", c.spec.Name, i+1),
			desc:       fmt.Sprintf("读取 MCP 资源 %s（%s）", r.Name, r.URI),
			schema:     schema,
			readOnly:   true,
			resource:   uri,
		})
	}
	return nil
}

// discoverPrompts registers every MCP prompt as a tool that returns the
// rendered prompt messages (mcp__<server>__prompt_<n>).
func (c *Client) discoverPrompts(ctx context.Context) error {
	result, err := c.trans.call(ctx, "prompts/list", nil)
	if err != nil {
		return nil
	}
	var list struct {
		Prompts []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Arguments   []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Required    bool   `json:"required"`
			} `json:"arguments"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, pr := range list.Prompts {
		props := map[string]any{}
		var required []string
		for _, a := range pr.Arguments {
			props[a.Name] = map[string]any{"type": "string", "description": a.Description}
			if a.Required {
				required = append(required, a.Name)
			}
		}
		schema := map[string]any{"type": "object", "properties": props, "required": required}
		schemaRaw, _ := json.Marshal(schema)
		c.tools = append(c.tools, &mcpTool{
			client:     c,
			serverTool: "prompt_" + fmt.Sprintf("%d", i+1),
			name:       fmt.Sprintf("mcp__%s__prompt_%d", c.spec.Name, i+1),
			desc:       pr.Description,
			schema:     schemaRaw,
			readOnly:   true,
			prompt:     pr.Name,
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
	readOnly   bool   // server-level annotation (Spec.ReadOnly); resources/prompts always true
	resource   string // fixed URI for resource tools
	prompt     string // original prompt name for prompt tools
}

func (t *mcpTool) Name() string            { return t.name }
func (t *mcpTool) Description() string     { return t.desc }
func (t *mcpTool) Schema() json.RawMessage { return t.schema }
func (t *mcpTool) ReadOnly() bool          { return t.readOnly }
func (t *mcpTool) Owner() tool.Owner       { return tool.Owner{Kind: "mcp", ID: t.client.spec.Name} }

func (t *mcpTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.resource != "" {
		result, err := t.client.trans.call(ctx, "resources/read", map[string]any{"uri": t.resource})
		if err != nil {
			return "", fmt.Errorf("mcp resource read: %w", err)
		}
		var resp struct {
			Contents []struct {
				Text string `json:"text"`
			} `json:"contents"`
		}
		if err := json.Unmarshal(result, &resp); err != nil {
			return string(result), nil
		}
		var out string
		for _, c := range resp.Contents {
			out += c.Text
		}
		return out, nil
	}
	if t.prompt != "" {
		callParams := map[string]any{"name": t.prompt}
		if len(args) > 0 && string(args) != "{}" {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(args, &raw); err == nil {
				callParams["arguments"] = raw
			}
		}
		result, err := t.client.trans.call(ctx, "prompts/get", callParams)
		if err != nil {
			return "", fmt.Errorf("mcp prompt get: %w", err)
		}
		var resp struct {
			Messages []struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(result, &resp); err != nil {
			return string(result), nil
		}
		var out string
		for _, m := range resp.Messages {
			out += m.Content.Text
		}
		return out, nil
	}
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
	cmd     *exec.Cmd
	stdin   *json.Encoder
	mu      sync.Mutex
	reqID   int
	kill    *time.Timer
	pending map[int]chan jsonrpcResponse
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
		cmd:     cmd,
		stdin:   json.NewEncoder(stdin),
		pending: make(map[int]chan jsonrpcResponse),
	}

	// Kill the process if it doesn't complete the handshake in time.
	t.kill = time.AfterFunc(time.Duration(timeout)*time.Second, func() {
		cmd.Process.Kill()
	})

	// A single long-lived reader goroutine decodes responses and routes them
	// to the caller registered for each request ID. This avoids leaking a
	// goroutine when a call is cancelled: the reader stays alive for the
	// lifetime of the transport.
	go t.readLoop(stdout)

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

	// Cancel the startup kill timer once a successful call happens.
	if t.kill != nil {
		t.kill.Stop()
		t.kill = nil
	}

	t.reqID++
	reqID := t.reqID
	ch := make(chan jsonrpcResponse, 1)
	t.pending[reqID] = ch
	req := jsonrpcRequest{JSONRPC: "2.0", ID: reqID, Method: method, Params: params}
	if err := t.stdin.Encode(req); err != nil {
		delete(t.pending, reqID)
		t.mu.Unlock()
		return nil, err
	}
	t.mu.Unlock()

	select {
	case <-ctx.Done():
		// Drop this caller's registration; the reader goroutine stays alive.
		t.mu.Lock()
		delete(t.pending, reqID)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("MCP transport closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// readLoop is the single stdout reader for this transport. Responses are
// routed to the pending channel registered for their request ID; responses
// with unknown IDs (notifications, events) are ignored.
func (t *stdioTransport) readLoop(stdout io.Reader) {
	dec := json.NewDecoder(stdout)
	for {
		var resp jsonrpcResponse
		if err := dec.Decode(&resp); err != nil {
			// Transport closed or broken — fail all in-flight calls.
			t.mu.Lock()
			for id, ch := range t.pending {
				delete(t.pending, id)
				close(ch)
			}
			t.mu.Unlock()
			return
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
