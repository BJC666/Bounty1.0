package plugin

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentDef is a subagent definition loaded from a markdown file.
type AgentDef struct {
	Name       string   `yaml:"name"`
	Description string  `yaml:"description"`
	Model      string   `yaml:"model"` // "inherit" | model name
	Tools      []string `yaml:"tools"` // allowed tools
	Color      string   `yaml:"color"` // UI color
	ReadOnly   bool     `yaml:"read_only"`
	Body       string
	SourcePath string
}

// AgentStore discovers and manages agent definitions.
type AgentStore struct {
	agents map[string]*AgentDef
}

func NewAgentStore() *AgentStore {
	return &AgentStore{agents: make(map[string]*AgentDef)}
}

// Discover scans directories for agent markdown files.
func (as *AgentStore) Discover(paths []string) error {
	for _, p := range paths {
		filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}

			name := strings.ToLower(info.Name())
			if strings.HasSuffix(name, ".md") {
				agent, err := parseAgentFile(path)
				if err == nil && agent != nil && agent.Name != "" {
					as.Register(agent)
				}
			}
			return nil
		})
	}
	return nil
}

func (as *AgentStore) Register(agent *AgentDef) {
	as.agents[agent.Name] = agent
}

func (as *AgentStore) Get(name string) *AgentDef {
	return as.agents[strings.ToLower(name)]
}

func (as *AgentStore) List() []*AgentDef {
	var result []*AgentDef
	for _, a := range as.agents {
		result = append(result, a)
	}
	return result
}

func parseAgentFile(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	var agent AgentDef
	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---\n")
		if end >= 0 {
			frontmatter := content[4 : end+4]
			yaml.Unmarshal([]byte(frontmatter), &agent)
			agent.Body = strings.TrimSpace(content[end+9:])
		}
	}
	if agent.Body == "" {
		agent.Body = content
	}
	agent.SourcePath = path
	return &agent, nil
}
