package skill

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Triggers    []string `yaml:"triggers"`
	RunAs       string   `yaml:"run_as"`
	Model       string   `yaml:"model"`
	Tools       []string `yaml:"tools"`
	ReadOnly    bool     `yaml:"read_only"`
	Body        string
	SourcePath  string
}

type IndexEntry struct {
	Name        string
	Description string
	IsSubagent  bool
}

type Store struct {
	skills   []*Skill
	enabled  []*Skill
	Rejected []RejectedSkill
}

func NewStore() *Store { return &Store{} }

func (s *Store) Discover(paths []string) error {
	for _, p := range paths {
		filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
				// parseSkillFile returns (nil, nil) for files without
				// frontmatter — skip those instead of appending a nil skill.
				sk, err := parseSkillFile(path)
				if err != nil || sk == nil {
					return nil
				}
				// P4-2: safety audit — skills carrying dangerous shell /
				// credential patterns are refused at load time and reported
				// via Store.Rejected instead of entering the index.
				if audit := AuditSkill(sk); !audit.Passed {
					s.Rejected = append(s.Rejected, RejectedSkill{
						Name: sk.Name, SourcePath: path, Findings: audit.Findings,
					})
					return nil
				}
				s.skills = append(s.skills, sk)
				s.enabled = append(s.enabled, sk)
			}
			return nil
		})
	}
	return nil
}

// Disable removes the named skills (config disabled_skills) from the active
// index. Unknown names are ignored.
func (s *Store) Disable(names []string) {
	blocked := make(map[string]bool, len(names))
	for _, n := range names {
		blocked[strings.ToLower(strings.TrimSpace(n))] = true
	}
	if len(blocked) == 0 {
		return
	}
	kept := s.enabled[:0]
	for _, sk := range s.enabled {
		if blocked[strings.ToLower(sk.Name)] {
			continue
		}
		kept = append(kept, sk)
	}
	s.enabled = kept
}

// Count returns the number of enabled skills in the index.
func (s *Store) Count() int { return len(s.enabled) }

func (s *Store) Index() []IndexEntry {
	var entries []IndexEntry
	for _, sk := range s.enabled {
		entries = append(entries, IndexEntry{
			Name: sk.Name, Description: sk.Description,
			IsSubagent: sk.RunAs == "subagent",
		})
	}
	return entries
}

func (s *Store) Get(name string) *Skill {
	for _, sk := range s.enabled {
		if strings.EqualFold(sk.Name, name) {
			return sk
		}
	}
	return nil
}

func parseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return nil, nil
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return nil, nil
	}
	frontmatter := content[4 : end+4]
	body := content[end+9:]
	var skill Skill
	if err := yaml.Unmarshal([]byte(frontmatter), &skill); err != nil {
		return nil, err
	}
	skill.Body = body
	skill.SourcePath = path
	return &skill, nil
}
