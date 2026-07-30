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
	owners map[string]Owner // tool name → owner
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Add(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = append(r.tools, t)
	r.dirty = true
	// Track ownership
	if owned, ok := t.(Owned); ok {
		if r.owners == nil {
			r.owners = make(map[string]Owner)
		}
		r.owners[t.Name()] = owned.Owner()
	}
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
// Each schema is injected with the tool's name and description.
func (r *Registry) Schemas() []json.RawMessage {
	r.mu.RLock()
	if !r.dirty {
		cached := r.cached
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

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
		raw := t.Schema()
		// Inject name and description into schema
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err == nil {
			m["name"] = t.Name()
			m["description"] = t.Description()
			if b, err := json.Marshal(m); err == nil {
				raw = b
			}
		}
		r.cached[i] = raw
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

// OwnerOf returns the owner of a tool, or Owner{Kind:"unknown"}.
func (r *Registry) OwnerOf(name string) Owner {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if o, ok := r.owners[name]; ok {
		return o
	}
	return Owner{Kind: "unknown"}
}

// ToolsByOwner returns tools grouped by owner kind.
func (r *Registry) ToolsByOwner() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string][]string)
	for _, t := range r.tools {
		kind := "unknown"
		if owned, ok := t.(Owned); ok {
			kind = owned.Owner().Kind
		}
		result[kind] = append(result[kind], t.Name())
	}
	return result
}
