package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Load loads config with priority: project > user > built-in defaults.
func Load(projectRoot string) (*Config, error) {
	cfg := Defaults()

	// 3. User config
	home, _ := os.UserHomeDir()
	if home != "" {
		userPath := filepath.Join(home, ".config", "bounty", "config.toml")
		loadTOML(userPath, cfg)
	}

	// 2. Project config
	if projectRoot != "" {
		projectPath := filepath.Join(projectRoot, "bounty.toml")
		loadTOML(projectPath, cfg)
	}

	return cfg, nil
}

// LoadFile loads a specific TOML file on top of defaults (no user/project merge).
func LoadFile(path string) (*Config, error) {
	cfg := Defaults()
	loadTOML(path, cfg)
	return cfg, nil
}

func loadTOML(path string, cfg *Config) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	var overlay Config
	if _, err := toml.DecodeFile(path, &overlay); err != nil {
		return
	}
	mergeConfig(cfg, &overlay)
}

func mergeConfig(dst, src *Config) {
	if src.DefaultModel != "" { dst.DefaultModel = src.DefaultModel }
	if len(src.Providers) > 0 { dst.Providers = src.Providers }
	if src.Agent.Temperature != 0 || src.Agent.CompactRatio != 0 { dst.Agent = src.Agent }
	if len(src.Sandbox.ForbidRead) > 0 || src.Sandbox.Bash != "" { dst.Sandbox = src.Sandbox }
	if len(src.Skills.Paths) > 0 { dst.Skills = src.Skills }
	if len(src.Plugins) > 0 { dst.Plugins = src.Plugins }
	if len(src.Permissions.Allow.Tools) > 0 || len(src.Permissions.Deny.BashPattern) > 0 {
		dst.Permissions = src.Permissions
	}
}
