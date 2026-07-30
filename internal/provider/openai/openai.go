package openai

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

type Provider struct {
	BaseURL    string
	APIKey     string
	Model      string
	MaxContext int
	client     *http.Client
}

func New(baseURL, apiKey, model string, maxContext int) *Provider {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if maxContext == 0 {
		maxContext = 1000000
	}
	return &Provider{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		MaxContext: maxContext,
		client:     &http.Client{},
	}
}

type chatRequest struct {
	Model       string            `json:"model"`
	Messages    []chatMessage     `json:"messages"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	Stream      bool              `json:"stream"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
	Name      string         `json:"name,omitempty"`
	ToolID    string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chunkResponse struct {
	Choices []struct {
		Delta struct {
			Role             string          `json:"role"`
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
			ToolCalls        []chunkToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type chunkToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type accToolCall struct {
	ID      string
	Name    string
	ArgsAcc bytes.Buffer
}

// Version returns a stable identifier for this provider implementation.
func (p *Provider) Version() string { return "openai-compat/1.0" }

func (p *Provider) Stream(ctx context.Context, messages []provider.Message, tools []json.RawMessage, opts provider.StreamOpts) (<-chan provider.StreamEvent, error) {
	chatMsgs := make([]chatMessage, len(messages))
	for i, m := range messages {
		cm := chatMessage{Role: m.Role, Content: m.Content, Name: m.ToolName, ToolID: m.ToolID}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
				ID: tc.ID, Type: "function",
				Function: chatFunction{Name: tc.Name, Arguments: string(tc.Args)},
			})
		}
		chatMsgs[i] = cm
	}

	reqBody := chatRequest{
		Model:       p.Model,
		Messages:    chatMsgs,
		Tools:       wrapTools(tools),
		Stream:      true,
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, classifyError(resp)
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

	var toolCallsAcc map[int]*accToolCall

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var chunk chunkResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		reasoning := choice.Delta.ReasoningContent
		content := choice.Delta.Content

		var tcds []provider.ToolCallDelta
		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index
			if toolCallsAcc == nil {
				toolCallsAcc = make(map[int]*accToolCall)
			}
			if _, ok := toolCallsAcc[idx]; !ok {
				toolCallsAcc[idx] = &accToolCall{}
			}
			acc := toolCallsAcc[idx]
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			acc.ArgsAcc.WriteString(tc.Function.Arguments)
			tcds = append(tcds, provider.ToolCallDelta{
				ID: acc.ID, Name: acc.Name, ArgsDelta: tc.Function.Arguments,
			})
		}

		if content != "" || reasoning != "" || len(tcds) > 0 {
			ch <- provider.StreamEvent{
				Delta: &provider.Delta{
					Content: content, Reasoning: reasoning, ToolCalls: tcds,
				},
			}
		}

		if chunk.Usage != nil {
			ch <- provider.StreamEvent{Usage: &provider.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}}
		}

		if choice.FinishReason != "" {
			ch <- provider.StreamEvent{Done: true}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- provider.StreamEvent{Err: fmt.Errorf("stream read error: %w", err)}
	}
}

// wrapTools wraps raw tool schemas in OpenAI function format.
// Each schema should have "name" and "description" at top level;
// parameters are the rest of the schema.
func wrapTools(tools []json.RawMessage) []json.RawMessage {
	wrapped := make([]json.RawMessage, len(tools))
	for i, raw := range tools {
		var schema map[string]interface{}
		if err := json.Unmarshal(raw, &schema); err != nil {
			wrapped[i] = raw
			continue
		}
		name, _ := schema["name"].(string)
		desc, _ := schema["description"].(string)
		// Remove name/desc from schema, keep as parameters
		delete(schema, "name")
		delete(schema, "description")
		if name == "" {
			name = fmt.Sprintf("tool_%d", i)
		}
		fn := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": desc,
				"parameters":  schema,
			},
		}
		b, _ := json.Marshal(fn)
		wrapped[i] = b
	}
	return wrapped
}
