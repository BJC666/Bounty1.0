package secrets

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Pool manages multiple API keys with rotation and exhaustion tracking.
type Pool struct {
	keys      []string
	current   int
	exhausted map[int]bool
	mu        sync.Mutex
}

// NewPool creates a pool from an env var value.
// Supports comma-separated multi-key: "KEY1,KEY2"
func NewPool(envVar string) (*Pool, error) {
	if envVar == "" {
		return nil, fmt.Errorf("api_key_env not set")
	}
	val := os.Getenv(envVar)
	if val == "" {
		return nil, fmt.Errorf("environment variable %s is empty", envVar)
	}
	keys := strings.Split(val, ",")
	var clean []string
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			clean = append(clean, k)
		}
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("no valid key in %s", envVar)
	}
	return &Pool{keys: clean, exhausted: make(map[int]bool)}, nil
}

// Get returns the current active key.
func (p *Pool) Get() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.keys) == 0 {
		return "", fmt.Errorf("no keys available")
	}
	// Find first non-exhausted key starting from current
	for i := 0; i < len(p.keys); i++ {
		idx := (p.current + i) % len(p.keys)
		if !p.exhausted[idx] {
			p.current = idx
			return p.keys[idx], nil
		}
	}
	return "", fmt.Errorf("all keys exhausted")
}

// Rotate switches to the next key. Call on 429 (rate limit).
func (p *Pool) Rotate() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = (p.current + 1) % len(p.keys)
	for i := 0; i < len(p.keys); i++ {
		idx := (p.current + i) % len(p.keys)
		if !p.exhausted[idx] {
			p.current = idx
			return p.keys[idx]
		}
	}
	return ""
}

// Exhaust marks the current key as exhausted. Call on 401/403 (auth error).
func (p *Pool) Exhaust() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exhausted[p.current] = true
	p.current = (p.current + 1) % len(p.keys)
}

// Available returns the count of non-exhausted keys.
func (p *Pool) Available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for i := range p.keys {
		if !p.exhausted[i] {
			count++
		}
	}
	return count
}

// LoadFromEnv is the simple single-key loader (kept for backward compat).
func LoadFromEnv(envVar string) (string, error) {
	pool, err := NewPool(envVar)
	if err != nil {
		return "", err
	}
	return pool.Get()
}
