package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LifecycleState is the state of a skill.
type LifecycleState string

const (
	StateActive   LifecycleState = "active"
	StateInactive LifecycleState = "inactive"
	StateArchived LifecycleState = "archived"
)

// SkillMeta tracks lifecycle metadata for a skill.
type SkillMeta struct {
	Name       string         `json:"name"`
	State      LifecycleState `json:"state"`
	Pinned     bool           `json:"pinned"`
	CreatedAt  time.Time      `json:"created_at"`
	LastUsedAt time.Time      `json:"last_used_at"`
	UseCount   int            `json:"use_count"`
}

// CuratorConfig holds curator settings.
type CuratorConfig struct {
	Enabled           bool `json:"enabled"`
	InactiveAfterDays int  `json:"inactive_after_days"` // default 30
	ArchiveAfterDays  int  `json:"archive_after_days"`  // default 90
	CheckIntervalDays int  `json:"check_interval_days"` // default 7
}

// Curator manages skill lifecycle.
type Curator struct {
	cfg   CuratorConfig
	meta  map[string]*SkillMeta // skill name -> meta
	path  string                // state file path
	store *Store
}

// NewCurator creates or loads a curator.
func NewCurator(cfg CuratorConfig, store *Store, dataDir string) *Curator {
	if cfg.InactiveAfterDays == 0 {
		cfg.InactiveAfterDays = 30
	}
	if cfg.ArchiveAfterDays == 0 {
		cfg.ArchiveAfterDays = 90
	}
	if cfg.CheckIntervalDays == 0 {
		cfg.CheckIntervalDays = 7
	}

	path := filepath.Join(dataDir, "curator_state.json")
	c := &Curator{cfg: cfg, meta: make(map[string]*SkillMeta), path: path, store: store}
	c.load()
	return c
}

func (c *Curator) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var meta []SkillMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return
	}
	for i := range meta {
		c.meta[meta[i].Name] = &meta[i]
	}
}

func (c *Curator) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return err
	}
	var list []SkillMeta
	for _, m := range c.meta {
		list = append(list, *m)
	}
	data, _ := json.MarshalIndent(list, "", "  ")
	return os.WriteFile(c.path, data, 0644)
}

// Touch records a skill use.
func (c *Curator) Touch(name string) {
	if m, ok := c.meta[name]; ok {
		m.LastUsedAt = time.Now()
		m.UseCount++
	} else {
		c.meta[name] = &SkillMeta{
			Name: name, State: StateActive,
			CreatedAt: time.Now(), LastUsedAt: time.Now(), UseCount: 1,
		}
	}
	c.save()
}

// Pin prevents a skill from auto-transitioning.
func (c *Curator) Pin(name string) error {
	m, ok := c.meta[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	m.Pinned = true
	return c.save()
}

// Unpin allows auto-transition for a skill.
func (c *Curator) Unpin(name string) error {
	m, ok := c.meta[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	m.Pinned = false
	return c.save()
}

// Run performs a lifecycle check. Call periodically (e.g., on agent startup).
func (c *Curator) Run() *CuratorReport {
	if !c.cfg.Enabled {
		return nil
	}

	now := time.Now()
	report := &CuratorReport{CheckedAt: now}

	for name, m := range c.meta {
		if m.Pinned {
			continue
		}

		daysSinceUse := int(now.Sub(m.LastUsedAt).Hours() / 24)

		switch m.State {
		case StateActive:
			if daysSinceUse > c.cfg.ArchiveAfterDays {
				m.State = StateArchived
				report.Archived = append(report.Archived, name)
			} else if daysSinceUse > c.cfg.InactiveAfterDays {
				m.State = StateInactive
				report.Inactivated = append(report.Inactivated, name)
			}
		case StateInactive:
			if daysSinceUse > c.cfg.ArchiveAfterDays {
				m.State = StateArchived
				report.Archived = append(report.Archived, name)
			} else if daysSinceUse <= c.cfg.InactiveAfterDays {
				m.State = StateActive
				report.Reactivated = append(report.Reactivated, name)
			}
		}
	}

	c.save()
	return report
}

// CuratorReport summarizes lifecycle changes.
type CuratorReport struct {
	CheckedAt   time.Time
	Inactivated []string
	Archived    []string
	Reactivated []string
}

func (r *CuratorReport) String() string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Skill Curator Report\n")
	if len(r.Inactivated) > 0 {
		sb.WriteString(fmt.Sprintf("- Inactivated (%d): %s\n", len(r.Inactivated), strings.Join(r.Inactivated, ", ")))
	}
	if len(r.Archived) > 0 {
		sb.WriteString(fmt.Sprintf("- Archived (%d): %s\n", len(r.Archived), strings.Join(r.Archived, ", ")))
	}
	if len(r.Reactivated) > 0 {
		sb.WriteString(fmt.Sprintf("- Reactivated (%d): %s\n", len(r.Reactivated), strings.Join(r.Reactivated, ", ")))
	}
	if len(r.Inactivated)+len(r.Archived)+len(r.Reactivated) == 0 {
		sb.WriteString("No changes.\n")
	}
	return sb.String()
}

// GetMeta returns metadata for a skill.
func (c *Curator) GetMeta(name string) *SkillMeta {
	return c.meta[name]
}

// AllMeta returns all tracked skill metadata.
func (c *Curator) AllMeta() []SkillMeta {
	var result []SkillMeta
	for _, m := range c.meta {
		result = append(result, *m)
	}
	return result
}
