package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Command is a slash command loaded from a markdown file.
type Command struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	ArgumentHint string   `yaml:"argument-hint"`
	AllowedTools []string `yaml:"allowed-tools"`
	Model        string   `yaml:"model"`
	Body         string
	SourcePath   string
}

// CommandStore discovers and manages slash commands.
type CommandStore struct {
	commands map[string]*Command
}

func NewCommandStore() *CommandStore {
	return &CommandStore{commands: make(map[string]*Command)}
}

// Discover scans directories for command markdown files.
// Format: commands/*.md or commands/<name>/COMMAND.md
func (cs *CommandStore) Discover(paths []string) error {
	for _, p := range paths {
		filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}

			name := strings.ToLower(info.Name())
			if strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, "skill.md") {
				cmd, err := parseCommandFile(path)
				if err == nil && cmd != nil {
					if cmd.Name == "" {
						cmd.Name = strings.TrimSuffix(info.Name(), ".md")
					}
					cs.Register(cmd)
				}
			}
			return nil
		})
	}
	return nil
}

// Register adds a command to the store.
func (cs *CommandStore) Register(cmd *Command) {
	cs.commands[cmd.Name] = cmd
}

// Get returns a command by name.
func (cs *CommandStore) Get(name string) *Command {
	return cs.commands[strings.TrimPrefix(strings.ToLower(name), "/")]
}

// List returns all registered commands.
func (cs *CommandStore) List() []*Command {
	var result []*Command
	for _, cmd := range cs.commands {
		result = append(result, cmd)
	}
	return result
}

// IndexBlock returns a cache-friendly command listing for the system prompt.
func (cs *CommandStore) IndexBlock() string {
	cmds := cs.List()
	if len(cmds) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Available Commands\n")
	for _, cmd := range cmds {
		arg := ""
		if cmd.ArgumentHint != "" {
			arg = " " + cmd.ArgumentHint
		}
		sb.WriteString(fmt.Sprintf("- /%s%s — %s\n", cmd.Name, arg, cmd.Description))
	}
	return sb.String()
}

func parseCommandFile(path string) (*Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	var cmd Command
	// Parse YAML frontmatter if present
	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---\n")
		if end >= 0 {
			frontmatter := content[4 : end+4]
			yaml.Unmarshal([]byte(frontmatter), &cmd)
			cmd.Body = strings.TrimSpace(content[end+9:])
		}
	}
	if cmd.Body == "" {
		cmd.Body = content
	}
	cmd.SourcePath = path
	return &cmd, nil
}
