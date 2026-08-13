package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"bounty/internal/tool"
)

// TestMain turns this test binary into a fake stdio MCP server when the
// BOUNTY_MCP_HELPER env var is set, so Host.Connect can be exercised against
// a real subprocess without external dependencies.
func TestMain(m *testing.M) {
	if os.Getenv("BOUNTY_MCP_HELPER") == "1" {
		runFakeStdioServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeStdioServer() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int            `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.ID == nil {
			continue // notification
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "fake", "version": "1.0.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name":        "echo",
				"description": "回显参数",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
					},
					"required": []string{"text"},
				},
			}}}
		case "tools/call":
			var p struct {
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			result = map[string]any{"content": []any{map[string]any{
				"type": "text", "text": "echo:" + string(p.Arguments),
			}}}
		case "resources/list":
			result = map[string]any{"resources": []any{map[string]any{
				"uri": "file:///data/readme.txt", "name": "readme", "description": "示例资源",
			}}}
		case "resources/read":
			result = map[string]any{"contents": []any{map[string]any{"text": "resource-content"}}}
		case "prompts/list":
			result = map[string]any{"prompts": []any{map[string]any{
				"name": "review", "description": "代码审查提示",
				"arguments": []any{map[string]any{"name": "code", "description": "待审代码", "required": true}},
			}}}
		case "prompts/get":
			result = map[string]any{"messages": []any{map[string]any{
				"role": "user", "content": map[string]any{"type": "text", "text": "prompt-content"},
			}}}
		default:
			result = map[string]any{}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}
}

func connectFakeStdio(t *testing.T) *Host {
	t.Helper()
	h := NewHost()
	t.Cleanup(h.Close)
	err := h.Connect(Spec{
		Name:    "fake",
		Command: os.Args[0],
		Env:     append(os.Environ(), "BOUNTY_MCP_HELPER=1"),
		Timeout: 60,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return h
}

func TestHostConnectStdioDiscoversToolsResourcesPrompts(t *testing.T) {
	h := connectFakeStdio(t)
	reg := tool.NewRegistry()
	h.RegisterTools(reg)

	for _, name := range []string{"mcp__fake__echo", "mcp__fake__resource_1", "mcp__fake__prompt_1"} {
		if got, ok := reg.Get(name); !ok {
			t.Fatalf("tool %s not registered", name)
		} else if got.ReadOnly() != (name != "mcp__fake__echo") {
			t.Fatalf("tool %s ReadOnly=%v, want resources/prompts read-only", name, got.ReadOnly())
		}
	}

	ctx := context.Background()
	echo, _ := reg.Get("mcp__fake__echo")
	out, err := echo.Execute(ctx, json.RawMessage(`{"text":"hi"}`))
	if err != nil || !strings.Contains(out, "echo:") {
		t.Fatalf("echo tool: out=%q err=%v", out, err)
	}

	res, _ := reg.Get("mcp__fake__resource_1")
	out, err = res.Execute(ctx, json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, "resource-content") {
		t.Fatalf("resource tool: out=%q err=%v", out, err)
	}

	pr, _ := reg.Get("mcp__fake__prompt_1")
	out, err = pr.Execute(ctx, json.RawMessage(`{"code":"x"}`))
	if err != nil || !strings.Contains(out, "prompt-content") {
		t.Fatalf("prompt tool: out=%q err=%v", out, err)
	}
}

func TestHostConnectReadOnlyServerFlag(t *testing.T) {
	h := NewHost()
	t.Cleanup(h.Close)
	err := h.Connect(Spec{
		Name:     "rofake",
		Command:  os.Args[0],
		Env:      append(os.Environ(), "BOUNTY_MCP_HELPER=1"),
		Timeout:  60,
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	reg := tool.NewRegistry()
	h.RegisterTools(reg)
	got, ok := reg.Get("mcp__rofake__echo")
	if !ok {
		t.Fatal("tool not registered")
	}
	if !got.ReadOnly() {
		t.Fatal("server-level ReadOnly flag not propagated to tools")
	}
}
