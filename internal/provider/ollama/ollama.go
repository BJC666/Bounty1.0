package ollama

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"bounty/internal/provider/openai"
)

// New creates an Ollama provider by wrapping the OpenAI-compatible client.
// Ollama exposes /v1/chat/completions at http://localhost:11434
func New(baseURL, model string) (*openai.Provider, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	// Query available models to validate
	resp, err := http.Get(strings.TrimRight(baseURL, "/") + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama not reachable at %s: %w (is 'ollama serve' running?)", baseURL, err)
	}
	defer resp.Body.Close()

	return openai.New(baseURL, "ollama-no-auth", model, 128000), nil
}

// ListModels fetches available models from Ollama.
func ListModels(baseURL string) ([]string, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	resp, err := http.Get(strings.TrimRight(baseURL, "/") + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct{ Name string } `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	var names []string
	for _, m := range result.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
