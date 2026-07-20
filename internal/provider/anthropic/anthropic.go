package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"bounty/internal/provider"
)

const (
	anthropicVersion = "2023-06-01"
	defaultBaseURL   = "https://api.anthropic.com"
)

type Provider struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxContext int
	MaxTokens  int
	client     *http.Client
}

func New(baseURL, apiKey, model string, maxContext int) *Provider {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if maxContext == 0 {
		maxContext = 200000
	}
	return &Provider{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		MaxContext: maxContext,
		MaxTokens:  8192,
		client:     &http.Client{},
	}
}

// ── Request types ──

type messagesRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	System      interface{}     `json:"system,omitempty"` // string or []textBlock
	Messages    []apiMessage    `json:"messages"`
	Tools       []apiTool       `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
	Temperature float64         `json:"temperature,omitempty"`
	Thinking    *thinkingConfig `json:"thinking,omitempty"`
}

type thinkingConfig struct {
	Type         string `json:"type"`          // "enabled"
	BudgetTokens int    `json:"budget_tokens"` // min 1024
}

type textBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

type apiMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []contentBlock
}

type contentBlock struct {
	Type         string      `json:"type"`                   // "text", "tool_use", "tool_result", "thinking"
	Text         string      `json:"text,omitempty"`
	ID           string      `json:"id,omitempty"`           // tool_use
	Name         string      `json:"name,omitempty"`         // tool_use
	Input        interface{} `json:"input,omitempty"`        // tool_use / tool_result
	ToolUseID    string      `json:"tool_use_id,omitempty"`  // tool_result
	Content      interface{} `json:"content,omitempty"`      // tool_result content
	Thinking     string      `json:"thinking,omitempty"`     // thinking block
	Signature    string      `json:"signature,omitempty"`    // thinking signature
	CacheControl *cacheCtrl  `json:"cache_control,omitempty"`
}

type cacheCtrl struct {
	Type string `json:"type"` // "ephemeral"
}

type apiTool struct {
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	InputSchema  interface{} `json:"input_schema"`
	CacheControl *cacheCtrl  `json:"cache_control,omitempty"`
}

// ── Response types ──

type sseEvent struct {
	Type         string            `json:"type"`
	Delta        *sseDelta         `json:"delta,omitempty"`
	Usage        *sseUsage         `json:"usage,omitempty"`
	Message      *sseMessage       `json:"message,omitempty"`
	ContentBlock *sseContentBlock  `json:"content_block,omitempty"`
}

type sseDelta struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

type sseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type sseMessage struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Model string `json:"model"`
}

type sseContentBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (p *Provider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	// Separate system prompt from messages
	var systemBlocks []textBlock
	var apiMsgs []apiMessage

	for _, m := range messages {
		if m.Role == "system" {
			systemBlocks = append(systemBlocks, textBlock{Type: "text", Text: m.Content})
			continue
		}
		am := apiMessage{Role: m.Role}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			var blocks []contentBlock
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input interface{}
				json.Unmarshal(tc.Args, &input)
				blocks = append(blocks, contentBlock{
					Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input,
				})
			}
			am.Content = blocks
		} else if m.Role == "tool" {
			am.Content = []contentBlock{{
				Type: "tool_result", ToolUseID: m.ToolID,
				Content: m.Content,
			}}
		} else {
			am.Content = m.Content
		}
		apiMsgs = append(apiMsgs, am)
	}

	// Convert tools to Anthropic format
	apiTools := make([]apiTool, len(tools))
	for i, t := range tools {
		var schema map[string]interface{}
		json.Unmarshal(t, &schema)
		apiTools[i] = apiTool{
			Name:        schemaName(schema),
			Description: schemaDesc(schema),
			InputSchema: schemaInput(schema),
		}
	}

	// Build system
	var system interface{}
	if len(systemBlocks) == 1 {
		system = systemBlocks[0].Text
	} else if len(systemBlocks) > 1 {
		blocks := make([]interface{}, len(systemBlocks))
		for i, b := range systemBlocks {
			blocks[i] = b
		}
		system = blocks
	}

	req := messagesRequest{
		Model:       p.Model,
		MaxTokens:   p.MaxTokens,
		System:      system,
		Messages:    apiMsgs,
		Tools:       apiTools,
		Stream:      true,
		Temperature: opts.Temperature,
	}

	if opts.Effort == "high" || opts.Effort == "max" {
		req.Thinking = &thinkingConfig{Type: "enabled", BudgetTokens: 4096}
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, classifyAnthropicError(resp)
	}

	ch := make(chan provider.StreamEvent, 10)
	go p.readStream(ctx, resp.Body, ch)
	return ch, nil
}

func (p *Provider) readStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var (
		currentToolID   string
		currentToolName string
		currentToolArgs strings.Builder
		textBuf         strings.Builder
		thinkingBuf     strings.Builder
	)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var ev sseEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock != nil {
				switch ev.ContentBlock.Type {
				case "text":
					textBuf.Reset()
				case "tool_use":
					currentToolID = ev.ContentBlock.ID
					currentToolName = ev.ContentBlock.Name
					currentToolArgs.Reset()
				case "thinking":
					thinkingBuf.Reset()
				}
			}

		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				ch <- provider.StreamEvent{Delta: &provider.Delta{Content: ev.Delta.Text}}
			case "thinking_delta":
				ch <- provider.StreamEvent{Delta: &provider.Delta{Reasoning: ev.Delta.Thinking}}
			case "input_json_delta":
				currentToolArgs.WriteString(ev.Delta.PartialJSON)
				ch <- provider.StreamEvent{Delta: &provider.Delta{
					ToolCalls: []provider.ToolCallDelta{{
						ID: currentToolID, Name: currentToolName,
						ArgsDelta: ev.Delta.PartialJSON,
					}},
				}}
			}

		case "message_delta":
			if ev.Usage != nil {
				ch <- provider.StreamEvent{Usage: &provider.Usage{
					InputTokens:  ev.Usage.InputTokens,
					OutputTokens: ev.Usage.OutputTokens,
				}}
			}

		case "message_stop":
			ch <- provider.StreamEvent{Done: true}
			return
		}
	}
}

// Helpers for tool schema conversion

func schemaName(schema map[string]interface{}) string {
	if s, ok := schema["name"].(string); ok {
		return s
	}
	return ""
}

func schemaDesc(schema map[string]interface{}) string {
	if d, ok := schema["description"].(string); ok {
		return d
	}
	return ""
}

func schemaInput(schema map[string]interface{}) interface{} {
	if fn, ok := schema["function"]; ok {
		if fnMap, ok := fn.(map[string]interface{}); ok {
			if params, ok := fnMap["parameters"]; ok {
				return params
			}
		}
	}
	// Return the raw schema minus name/description
	result := make(map[string]interface{})
	for k, v := range schema {
		if k != "name" && k != "description" {
			result[k] = v
		}
	}
	return result
}
