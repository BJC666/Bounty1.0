package tool

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) (string, error)
	ReadOnly() bool
}

// Owner describes the source of a tool.
type Owner struct {
	Kind string // "core", "plugin", "mcp"
	ID   string
}

// Owned is an optional interface — tools may implement it to declare provenance.
type Owned interface {
	Owner() Owner
}

// Registry manages the active tool set. Thread-safe.
type Registry struct {
	mu     sync.RWMutex
	tools  []Tool
	cached []json.RawMessage
	dirty  bool
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Add(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = append(r.tools, t)
	r.dirty = true
}

func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		if t.Name() != name {
			filtered = append(filtered, t)
		}
	}
	r.tools = filtered
	r.dirty = true
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// Schemas returns JSON Schemas sorted by name — byte-stable (cache-friendly).
func (r *Registry) Schemas() []json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.dirty {
		return r.cached
	}
	sort.Slice(r.tools, func(i, j int) bool {
		return r.tools[i].Name() < r.tools[j].Name()
	})
	r.cached = make([]json.RawMessage, len(r.tools))
	for i, t := range r.tools {
		r.cached[i] = t.Schema()
	}
	r.dirty = false
	return r.cached
}

func (r *Registry) ReadOnlyTools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Tool
	for _, t := range r.tools {
		if t.ReadOnly() {
			result = append(result, t)
		}
	}
	return result
}

func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, len(r.tools))
	copy(result, r.tools)
	return result
}
