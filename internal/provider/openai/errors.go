package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func classifyError(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)

	switch {
	case resp.StatusCode == 429 || strings.Contains(bodyStr, "rate_limit"):
		return &RetryableError{Category: "RateLimit", Message: bodyStr, MaxRetries: 5,
			BackoffFunc: exponentialBackoff}
	case resp.StatusCode == 400 && strings.Contains(bodyStr, "context_length"):
		return &RetryableError{Category: "ContextOverflow", Message: bodyStr, MaxRetries: 1}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return &RetryableError{Category: "AuthError", Message: bodyStr, MaxRetries: 0}
	case resp.StatusCode >= 500:
		return &RetryableError{Category: "ServerError", Message: bodyStr, MaxRetries: 3,
			BackoffFunc: linearBackoff}
	case resp.StatusCode == 400 && strings.Contains(bodyStr, "content_filter"):
		return &RetryableError{Category: "ContentFilter", Message: bodyStr, MaxRetries: 1}
	default:
		return &FatalError{Category: "FatalError", Message: bodyStr}
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
