package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"bounty/internal/tool"
)

type WebFetchTool struct{}

func (WebFetchTool) Name() string        { return "web_fetch" }
func (WebFetchTool) ReadOnly() bool      { return true }
func (WebFetchTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "builtin"} }
func (WebFetchTool) Description() string { return "Fetches a URL and returns its content as plain text." }
func (WebFetchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","format":"uri","description":"The URL to fetch"}},"required":["url"]}`)
}
func (WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct{ URL string `json:"url"` }
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	out := string(body)
	if len(out) > 32000 {
		runes := []rune(out)
		if len(runes) > 32000/4 {
			out = string(runes[:32000/4]) + "\n... [truncated]"
		} else {
			out = out[:32000] + "\n... [truncated]"
		}
	}
	return out, nil
}
