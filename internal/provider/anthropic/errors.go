package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func classifyAnthropicError(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)

	var ae anthropicError
	json.Unmarshal(bodyBytes, &ae)
	errType := ae.Error.Type
	if errType == "" {
		errType = bodyStr
	}

	switch {
	case resp.StatusCode == 429:
		return &RetryableError{Category: "RateLimit", Message: errType, MaxRetries: 5,
			BackoffFunc: exponentialBackoff}
	case resp.StatusCode == 400 && (strings.Contains(errType, "context") || strings.Contains(errType, "token")):
		return &RetryableError{Category: "ContextOverflow", Message: errType, MaxRetries: 1}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return &RetryableError{Category: "AuthError", Message: errType, MaxRetries: 0}
	case resp.StatusCode >= 500:
		return &RetryableError{Category: "ServerError", Message: errType, MaxRetries: 3,
			BackoffFunc: linearBackoff}
	default:
		return &FatalError{Category: "FatalError", Message: errType}
	}
}

type RetryableError struct {
	Category    string
	Message     string
	MaxRetries  int
	BackoffFunc func(attempt int) time.Duration
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Category, e.Message)
}

type FatalError struct {
	Category string
	Message  string
}

func (e *FatalError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Category, e.Message)
}

func exponentialBackoff(attempt int) time.Duration {
	base := 1 * time.Second
	for i := 0; i < attempt; i++ {
		base *= 2
	}
	if base > 60*time.Second {
		base = 60 * time.Second
	}
	return base
}

func linearBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * 5 * time.Second
}
