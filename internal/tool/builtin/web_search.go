package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"bounty/internal/tool"
)

type WebSearchTool struct {
	client *http.Client
}

func (w *WebSearchTool) Name() string   { return "web_search" }
func (w *WebSearchTool) ReadOnly() bool { return true }
func (w *WebSearchTool) Owner() tool.Owner { return tool.Owner{Kind: "core", ID: "builtin"} }
func (w *WebSearchTool) Description() string {
	return "Search the web. Returns result blocks with titles, URLs, and snippets."
}
func (w *WebSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"The search query"},"allowed_domains":{"type":"array","items":{"type":"string"}},"blocked_domains":{"type":"array","items":{"type":"string"}}},"required":["query"]}`)
}

func (w *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query          string   `json:"query"`
		AllowedDomains []string `json:"allowed_domains"`
		BlockedDomains []string `json:"blocked_domains"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if w.client == nil {
		w.client = http.DefaultClient
	}

	// Try DuckDuckGo Instant Answer API first (free, no key)
	results := w.searchDDG(ctx, params.Query)

	// Filter by allowed/blocked domains
	filtered := make([]searchResult, 0, len(results))
	for _, r := range results {
		if containsAnyDomain(r.URL, params.BlockedDomains) {
			continue
		}
		if len(params.AllowedDomains) > 0 && !containsAnyDomain(r.URL, params.AllowedDomains) {
			continue
		}
		filtered = append(filtered, r)
	}
	results = filtered

	if len(results) > 0 {
		return formatResults(params.Query, results), nil
	}
	return fmt.Sprintf("No results found for: %s", params.Query), nil
}

type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

func (w *WebSearchTool) searchDDG(ctx context.Context, query string) []searchResult {
	apiURL := "https://api.duckduckgo.com/?q=" + url.QueryEscape(query) + "&format=json&no_html=1&skip_disambig=1"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "BountyAgent/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))

	var ddgResp struct {
		AbstractText string `json:"AbstractText"`
		AbstractURL  string `json:"AbstractURL"`
		Heading      string `json:"Heading"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
		Results []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(body, &ddgResp); err != nil {
		return nil
	}

	var results []searchResult

	// Abstract (instant answer)
	if ddgResp.AbstractText != "" {
		results = append(results, searchResult{
			Title:   ddgResp.Heading,
			URL:     ddgResp.AbstractURL,
			Snippet: ddgResp.AbstractText,
		})
	}

	// Related topics
	for _, t := range ddgResp.RelatedTopics {
		if len(results) >= 10 {
			break
		}
		parts := strings.SplitN(t.Text, " - ", 2)
		title := parts[0]
		snippet := ""
		if len(parts) > 1 {
			snippet = parts[1]
		}
		results = append(results, searchResult{Title: title, URL: t.FirstURL, Snippet: snippet})
	}

	// External results
	for _, r := range ddgResp.Results {
		if len(results) >= 10 {
			break
		}
		results = append(results, searchResult{Title: r.Text, URL: r.FirstURL})
	}

	return results
}

// containsAnyDomain reports whether the URL's host matches any of the given
// domains, honoring subdomain boundaries: "example.com" matches
// "www.example.com" but not "evil-example.com".
func containsAnyDomain(rawURL string, domains []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func formatResults(query string, results []searchResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s\n\n", query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
