package provider

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// PrefixShape hashes the cache-stable components of a provider request.
// Comparing shapes between turns explains cache hits and misses.
type PrefixShape struct {
	SystemPromptHash string `json:"system_prompt_hash"`
	ToolsHash        string `json:"tools_hash"`
	ProviderVersion  string `json:"provider_version"`
}

// ComputeShape hashes the stable prefix components.
func ComputeShape(systemPrompt string, tools []json.RawMessage, providerVersion string) PrefixShape {
	return PrefixShape{
		SystemPromptHash: hashString(systemPrompt),
		ToolsHash:        hashTools(tools),
		ProviderVersion:  providerVersion,
	}
}

// Compare returns nil if shapes are identical, or a description of what changed.
func (p PrefixShape) Compare(prev PrefixShape) *CacheMissReason {
	if p == prev {
		return nil
	}
	reason := &CacheMissReason{}
	if p.SystemPromptHash != prev.SystemPromptHash {
		reason.Reasons = append(reason.Reasons, "system prompt changed")
	}
	if p.ToolsHash != prev.ToolsHash {
		reason.Reasons = append(reason.Reasons, "tools changed (schema or count)")
	}
	if p.ProviderVersion != prev.ProviderVersion {
		reason.Reasons = append(reason.Reasons, "provider/version changed")
	}
	if len(reason.Reasons) == 0 {
		reason.Reasons = append(reason.Reasons, "unknown (shape hash differs but no known component changed)")
	}
	return reason
}

// CacheMissReason describes why a cache miss occurred.
type CacheMissReason struct {
	Reasons []string `json:"reasons"`
}

func (c *CacheMissReason) String() string {
	if c == nil {
		return "cache hit"
	}
	s := "cache miss: "
	for i, r := range c.Reasons {
		if i > 0 {
			s += "; "
		}
		s += r
	}
	return s
}

// CacheStats tracks aggregate cache performance.
type CacheStats struct {
	TotalRequests int
	CacheHits     int
	CacheMisses   int
	LastMiss      *CacheMissReason
}

// HashMessages hashes a message sequence (used for cached-prefix identity).
func HashMessages(msgs []Message) string {
	h := sha256.New()
	for _, m := range msgs {
		write := func(s string) {
			h.Write([]byte(s))
			h.Write([]byte{0})
		}
		write(m.Role)
		write(m.Content)
		write(m.ToolName)
		write(m.ToolID)
		for _, tc := range m.ToolCalls {
			write(tc.ID)
			write(tc.Name)
			h.Write(tc.Args)
			h.Write([]byte{0})
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:8])
}

// RecordHit registers a request whose stable prefix was reused.
func (cs *CacheStats) RecordHit() {
	cs.TotalRequests++
	cs.CacheHits++
}

// RecordMiss registers a request whose stable prefix changed.
func (cs *CacheStats) RecordMiss(reason *CacheMissReason) {
	cs.TotalRequests++
	cs.CacheMisses++
	if reason != nil {
		cs.LastMiss = reason
	}
}

// Record compares two shapes and updates the stats.
func (cs *CacheStats) Record(prev, curr PrefixShape) {
	cs.TotalRequests++
	if prev.Compare(curr) == nil {
		cs.CacheHits++
	} else {
		cs.CacheMisses++
		cs.LastMiss = prev.Compare(curr)
	}
}

// HitRate returns the cache hit rate as a fraction (0.0 to 1.0).
func (cs *CacheStats) HitRate() float64 {
	if cs.TotalRequests == 0 {
		return 0
	}
	return float64(cs.CacheHits) / float64(cs.TotalRequests)
}

// Summary returns a human-readable cache performance summary.
func (cs *CacheStats) Summary() string {
	return fmt.Sprintf("Cache: %.1f%% hit rate (%d/%d requests)",
		cs.HitRate()*100, cs.CacheHits, cs.TotalRequests)
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

func hashTools(tools []json.RawMessage) string {
	// Sort for deterministic hashing
	sorted := make([]string, len(tools))
	for i, t := range tools {
		sorted[i] = string(t)
	}
	sort.Strings(sorted)
	h := sha256.New()
	for _, s := range sorted {
		h.Write([]byte(s))
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:8])
}
