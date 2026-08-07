package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// GraphNode is a node in the learning graph.
type GraphNode struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"` // "skill", "memory", "tool", "concept"
	Label     string            `json:"label"`
	CreatedAt time.Time         `json:"created_at"`
	UsedCount int               `json:"used_count"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// GraphEdge is a weighted edge between two nodes.
type GraphEdge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
	Type   string  `json:"type"` // "uses", "related_to", "derived_from"
}

// LearningGraph tracks relationships between learned items.
type LearningGraph struct {
	mu    sync.Mutex
	Nodes map[string]*GraphNode `json:"nodes"`
	Edges []GraphEdge           `json:"edges"`
	path  string
}

// NewLearningGraph creates or loads a learning graph.
func NewLearningGraph(dataDir string) *LearningGraph {
	lg := &LearningGraph{
		Nodes: make(map[string]*GraphNode),
		path:  filepath.Join(dataDir, "learning_graph.json"),
	}
	lg.load()
	return lg
}

func (lg *LearningGraph) load() {
	data, err := os.ReadFile(lg.path)
	if err != nil {
		return
	}
	var saved struct {
		Nodes map[string]*GraphNode `json:"nodes"`
		Edges []GraphEdge           `json:"edges"`
	}
	if json.Unmarshal(data, &saved) == nil {
		lg.Nodes = saved.Nodes
		lg.Edges = saved.Edges
	}
	// A persisted file with "nodes": null (or an empty object) must not leave
	// the map nil — later AddNode calls would panic.
	if lg.Nodes == nil {
		lg.Nodes = make(map[string]*GraphNode)
	}
	if lg.Edges == nil {
		lg.Edges = []GraphEdge{}
	}
}

func (lg *LearningGraph) save() error {
	if err := os.MkdirAll(filepath.Dir(lg.path), 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(lg, "", "  ")
	return os.WriteFile(lg.path, data, 0644)
}

// AddNode adds or updates a node.
func (lg *LearningGraph) AddNode(id, nodeType, label string) *GraphNode {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	if n, ok := lg.Nodes[id]; ok {
		n.UsedCount++
		lg.save()
		return n
	}
	n := &GraphNode{
		ID:        id,
		Type:      nodeType,
		Label:     label,
		CreatedAt: time.Now(),
		UsedCount: 1,
	}
	lg.Nodes[id] = n
	lg.save()
	return n
}

// AddEdge adds a weighted edge between two nodes.
func (lg *LearningGraph) AddEdge(source, target, edgeType string, weight float64) {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	// Check if edge already exists
	for i, e := range lg.Edges {
		if e.Source == source && e.Target == target && e.Type == edgeType {
			lg.Edges[i].Weight = (e.Weight + weight) / 2 // running average
			lg.save()
			return
		}
	}
	lg.Edges = append(lg.Edges, GraphEdge{
		Source: source, Target: target, Type: edgeType, Weight: weight,
	})
	lg.save()
}

// Touch records usage of a node, creating it if needed.
func (lg *LearningGraph) Touch(id, nodeType, label string) {
	lg.AddNode(id, nodeType, label)
}

// LinkFromSkills creates edges from skills to tools/concepts based on co-occurrence.
func (lg *LearningGraph) LinkFromSkills(skillName string, toolsUsed []string) {
	for _, tool := range toolsUsed {
		toolID := "tool:" + tool
		lg.AddNode(toolID, "tool", tool)
		lg.AddEdge("skill:"+skillName, toolID, "uses", 1.0)
	}
}

// LinkFromMemory creates edges from memory entries to concepts.
func (lg *LearningGraph) LinkFromMemory(memoryName string, concepts []string) {
	for _, concept := range concepts {
		conceptID := "concept:" + concept
		lg.AddNode(conceptID, "concept", concept)
		lg.AddEdge("memory:"+memoryName, conceptID, "related_to", 0.5)
	}
}

// RelatedTo returns nodes related to the given node.
func (lg *LearningGraph) RelatedTo(nodeID string, minWeight float64) []GraphEdge {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	var result []GraphEdge
	for _, e := range lg.Edges {
		if (e.Source == nodeID || e.Target == nodeID) && e.Weight >= minWeight {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Weight > result[j].Weight })
	return result
}

// TopNodes returns the most-used nodes of a given type.
func (lg *LearningGraph) TopNodes(nodeType string, limit int) []*GraphNode {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	var nodes []*GraphNode
	for _, n := range lg.Nodes {
		if n.Type == nodeType {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].UsedCount > nodes[j].UsedCount })
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes
}

// Summary returns a human-readable summary of the learning graph.
func (lg *LearningGraph) Summary() string {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("## Learning Graph\n")
	sb.WriteString(fmt.Sprintf("- Nodes: %d\n", len(lg.Nodes)))
	sb.WriteString(fmt.Sprintf("- Edges: %d\n", len(lg.Edges)))

	// Top skills
	skills := lg.topNodesLocked("skill", 5)
	if len(skills) > 0 {
		sb.WriteString("\n### Top Skills\n")
		for _, s := range skills {
			sb.WriteString(fmt.Sprintf("- %s (%d uses)\n", s.Label, s.UsedCount))
		}
	}

	// Top tools
	tools := lg.topNodesLocked("tool", 5)
	if len(tools) > 0 {
		sb.WriteString("\n### Top Tools\n")
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("- %s (%d uses)\n", t.Label, t.UsedCount))
		}
	}

	// Strongest relationships
	if len(lg.Edges) > 0 {
		sb.WriteString("\n### Strongest Relationships\n")
		edges := make([]GraphEdge, len(lg.Edges))
		copy(edges, lg.Edges)
		sort.Slice(edges, func(i, j int) bool { return edges[i].Weight > edges[j].Weight })
		for i, e := range edges {
			if i >= 5 {
				break
			}
			sourceLabel := nodeLabel(lg.Nodes[e.Source], e.Source)
			targetLabel := nodeLabel(lg.Nodes[e.Target], e.Target)
			sb.WriteString(fmt.Sprintf("- %s → %s (%.1f)\n", sourceLabel, targetLabel, e.Weight))
		}
	}

	return sb.String()
}

// nodeLabel returns the label of a graph node, falling back to its ID when
// the node is missing (e.g. an edge referencing a pruned node).
func nodeLabel(n *GraphNode, id string) string {
	if n == nil {
		return id
	}
	return n.Label
}

// topNodesLocked is the internal, already-locked variant of TopNodes.
func (lg *LearningGraph) topNodesLocked(nodeType string, limit int) []*GraphNode {
	var nodes []*GraphNode
	for _, n := range lg.Nodes {
		if n.Type == nodeType {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].UsedCount > nodes[j].UsedCount })
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes
}

// ExportDOT returns the graph in DOT format for visualization tools.
func (lg *LearningGraph) ExportDOT() string {
	lg.mu.Lock()
	defer lg.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("digraph LearningGraph {\n")
	sb.WriteString("  rankdir=LR;\n")

	// Nodes with colors by type
	colors := map[string]string{
		"skill":   "#4CAF50",
		"memory":  "#2196F3",
		"tool":    "#FF9800",
		"concept": "#9C27B0",
	}
	for _, n := range lg.Nodes {
		color := colors[n.Type]
		if color == "" {
			color = "#999999"
		}
		sb.WriteString(fmt.Sprintf("  %q [label=%q, style=filled, fillcolor=%q, fontsize=%d];\n",
			n.ID, n.Label, color, 10+n.UsedCount))
	}

	// Edges
	for _, e := range lg.Edges {
		penwidth := e.Weight * 2
		if penwidth < 1 {
			penwidth = 1
		}
		sb.WriteString(fmt.Sprintf("  %q -> %q [label=%q, penwidth=%.1f];\n",
			e.Source, e.Target, e.Type, penwidth))
	}
	sb.WriteString("}\n")
	return sb.String()
}
