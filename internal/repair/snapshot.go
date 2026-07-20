package repair

import (
	"fmt"
	"os"
	"path/filepath"

	"bounty/internal/config"
)

const snapshotName = "config.toml.snapshot"

// SnapshotPath returns the path for the last-known-good config snapshot.
func SnapshotPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "bounty", snapshotName)
}

// SaveSnapshot writes a copy of the current config as last-known-good.
func SaveSnapshot(cfg *config.Config) error {
	path := SnapshotPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write a simple TOML representation of the essential parts
	fmt.Fprintf(f, "# Bounty last-known-good config snapshot\n")
	fmt.Fprintf(f, "config_version = %d\n", cfg.Version)
	fmt.Fprintf(f, "default_model = %q\n", cfg.DefaultModel)
	for _, p := range cfg.Providers {
		fmt.Fprintf(f, "\n[[providers]]\n")
		fmt.Fprintf(f, "name = %q\n", p.Name)
		fmt.Fprintf(f, "kind = %q\n", p.Kind)
		if p.BaseURL != "" {
			fmt.Fprintf(f, "base_url = %q\n", p.BaseURL)
		}
		fmt.Fprintf(f, "api_key_env = %q\n", p.APIKeyEnv)
		if p.ContextWindow > 0 {
			fmt.Fprintf(f, "context_window = %d\n", p.ContextWindow)
		}
	}
	return nil
}

// RestoreSnapshot attempts to load the last-known-good config.
func RestoreSnapshot() (*config.Config, error) {
	path := SnapshotPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("no snapshot found at %s", path)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("snapshot is also corrupt: %w", err)
	}
	return cfg, nil
}

// SafeLoad attempts to load config; on failure, tries snapshot.
func SafeLoad(projectRoot string) (*config.Config, error) {
	cfg, err := config.Load(projectRoot)
	if err == nil {
		// Save successful load as new snapshot
		if saveErr := SaveSnapshot(cfg); saveErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save config snapshot: %v\n", saveErr)
		}
		return cfg, nil
	}

	// Try restore from snapshot
	fmt.Fprintf(os.Stderr, "Config load failed: %v\nTrying last-known-good snapshot...\n", err)
	snapCfg, snapErr := RestoreSnapshot()
	if snapErr != nil {
		return nil, fmt.Errorf("config load failed (%w) and snapshot unavailable (%w)", err, snapErr)
	}
	fmt.Fprintf(os.Stderr, "Restored from snapshot.\n")
	return snapCfg, nil
}
