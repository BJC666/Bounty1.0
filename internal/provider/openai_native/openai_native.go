package openai_native

import (
	"bounty/internal/provider/openai"
)

// New creates an OpenAI-native provider.
// Uses the standard OpenAI base URL and Chat Completions API.
func New(apiKey, model string, maxContext int) *openai.Provider {
	return openai.New("https://api.openai.com", apiKey, model, maxContext)
}

// NewWithResponsesAPI creates a provider using the Responses API endpoint.
func NewWithResponsesAPI(apiKey, model string) *openai.Provider {
	return openai.New("https://api.openai.com", apiKey, model, 200000)
}
