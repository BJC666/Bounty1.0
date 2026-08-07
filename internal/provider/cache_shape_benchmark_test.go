package provider

import (
	"encoding/json"
	"testing"
)

// BenchmarkComputeShape measures the cost of hashing the cache-stable prefix
// (system prompt + tools) on every turn — the hot path for cache decisions.
func BenchmarkComputeShape(b *testing.B) {
	systemPrompt := `You are Bounty, a security-first coding agent. ` + string(make([]byte, 4096))
	tools := []json.RawMessage{
		json.RawMessage(`{"name":"bash"}`),
		json.RawMessage(`{"name":"read_file"}`),
		json.RawMessage(`{"name":"write_file"}`),
		json.RawMessage(`{"name":"edit_file"}`),
		json.RawMessage(`{"name":"grep"}`),
		json.RawMessage(`{"name":"glob"}`),
		json.RawMessage(`{"name":"todo_write"}`),
		json.RawMessage(`{"name":"web_fetch"}`),
		json.RawMessage(`{"name":"web_search"}`),
		json.RawMessage(`{"name":"code_index"}`),
		json.RawMessage(`{"name":"remember"}`),
		json.RawMessage(`{"name":"browser"}`),
		json.RawMessage(`{"name":"task"}`),
		json.RawMessage(`{"name":"read_only_task"}`),
		json.RawMessage(`{"name":"fleet"}`),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ComputeShape(systemPrompt, tools, "test-v1")
	}
}

// TestCacheStatsStablePrefixHitRate simulates a long conversation where the
// cache-stable prefix (system prompt + tools) never changes. In real prompt
// caching, that yields ~100% prefix hits; this test asserts the shape-based
// tracking agrees.
func TestCacheStatsStablePrefixHitRate(t *testing.T) {
	sys := "system prompt (stable across the whole session)"
	tools := []json.RawMessage{json.RawMessage(`{"name":"bash"}`), json.RawMessage(`{"name":"grep"}`)}
	first := ComputeShape(sys, tools, "v1")

	var cs CacheStats
	const turns = 4350 // simulated long session (scaled down from 435M tokens)
	for i := 0; i < turns; i++ {
		cs.Record(first, first)
	}
	if cs.TotalRequests != turns {
		t.Fatalf("TotalRequests = %d, want %d", cs.TotalRequests, turns)
	}
	if cs.CacheHits != turns {
		t.Fatalf("CacheHits = %d, want %d (stable prefix must hit every turn)", cs.CacheHits, turns)
	}
	rate := float64(cs.CacheHits) / float64(cs.TotalRequests) * 100
	if rate < 99.0 {
		t.Fatalf("hit rate = %.2f%%, want >= 99%%", rate)
	}
	t.Logf("stable-prefix simulated hit rate: %.2f%% over %d turns", rate, turns)
}

// TestCacheStatsToolOrderStable verifies the shape hasher is order-insensitive
// (schemas are sorted before hashing), so a tool-order shuffle stays a cache
// hit — the design goal behind "cache-stable prefix".
func TestCacheStatsToolOrderStable(t *testing.T) {
	sys := "stable system prompt"
	toolsA := []json.RawMessage{json.RawMessage(`{"name":"bash"}`), json.RawMessage(`{"name":"grep"}`)}
	toolsB := []json.RawMessage{json.RawMessage(`{"name":"grep"}`), json.RawMessage(`{"name":"bash"}`)}
	a := ComputeShape(sys, toolsA, "v1")
	b := ComputeShape(sys, toolsB, "v1")

	var cs CacheStats
	cs.Record(a, a)
	cs.Record(a, b) // same tool set, different order → must stay a hit
	if cs.CacheHits != 2 || cs.CacheMisses != 0 {
		t.Fatalf("hits=%d misses=%d, want 2/0 (order-insensitive)", cs.CacheHits, cs.CacheMisses)
	}
}

// TestCacheStatsToolContentChangeMiss verifies a real schema change (tool added)
// breaks the prefix and is reported as a miss with a reason.
func TestCacheStatsToolContentChangeMiss(t *testing.T) {
	sys := "stable system prompt"
	toolsA := []json.RawMessage{json.RawMessage(`{"name":"bash"}`)}
	toolsB := []json.RawMessage{json.RawMessage(`{"name":"bash"}`), json.RawMessage(`{"name":"grep"}`)}
	a := ComputeShape(sys, toolsA, "v1")
	b := ComputeShape(sys, toolsB, "v1")

	var cs CacheStats
	cs.Record(a, a)
	cs.Record(a, b) // tool set grew → must be a miss
	if cs.CacheHits != 1 || cs.CacheMisses != 1 {
		t.Fatalf("hits=%d misses=%d, want 1/1", cs.CacheHits, cs.CacheMisses)
	}
	if cs.LastMiss == nil || cs.LastMiss.String() == "cache hit" {
		t.Fatal("expected a miss reason for tool change")
	}
	t.Log("miss reason:", cs.LastMiss.String())
}

