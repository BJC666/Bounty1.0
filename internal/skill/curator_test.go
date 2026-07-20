package skill

import (
	"testing"
	"time"
)

func TestCuratorLifecycle(t *testing.T) {
	dir := t.TempDir()
	cfg := CuratorConfig{
		Enabled:           true,
		InactiveAfterDays: 1,
		ArchiveAfterDays:  2,
	}
	c := NewCurator(cfg, NewStore(), dir)

	// Register a skill
	c.Touch("test-skill")

	// Simulate old usage
	c.meta["test-skill"].LastUsedAt = time.Now().AddDate(0, 0, -3)

	report := c.Run()
	if len(report.Archived) != 1 {
		t.Errorf("expected 1 archived, got %v", report.Archived)
	}
}

func TestCuratorPin(t *testing.T) {
	dir := t.TempDir()
	cfg := CuratorConfig{Enabled: true, InactiveAfterDays: 1, ArchiveAfterDays: 2}
	c := NewCurator(cfg, NewStore(), dir)

	c.Touch("pinned-skill")
	c.Pin("pinned-skill")
	c.meta["pinned-skill"].LastUsedAt = time.Now().AddDate(0, 0, -5)

	report := c.Run()
	if len(report.Archived) > 0 {
		t.Error("pinned skill should not be archived")
	}
}
