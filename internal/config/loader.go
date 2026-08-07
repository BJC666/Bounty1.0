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

// mergeConfig overlays non-zero fields from src onto dst. Field-level merging
// (rather than whole-struct replacement) keeps defaults that the user did not
// explicitly override, and ensures sections like Hooks/Language merge too.
func mergeConfig(dst, src *Config) {
	if src.Version != 0 {
		dst.Version = src.Version
	}
	if src.DefaultModel != "" {
		dst.DefaultModel = src.DefaultModel
	}
	if src.Language != "" {
		dst.Language = src.Language
	}
	if len(src.Providers) > 0 {
		dst.Providers = src.Providers
	}
	mergeAgent(&dst.Agent, &src.Agent)
	mergeSandbox(&dst.Sandbox, &src.Sandbox)
	mergeSkills(&dst.Skills, &src.Skills)
	mergePermissions(&dst.Permissions, &src.Permissions)
	if len(src.Plugins) > 0 {
		dst.Plugins = src.Plugins
	}
	if src.Hooks.Enabled {
		dst.Hooks.Enabled = true
	}
	if len(src.Hooks.Shell) > 0 {
		dst.Hooks.Shell = src.Hooks.Shell
	}
}

func mergeAgent(dst, src *AgentConfig) {
	if src.Temperature != 0 {
		dst.Temperature = src.Temperature
	}
	if src.CompactRatio != 0 {
		dst.CompactRatio = src.CompactRatio
	}
	if src.CompactForceRatio != 0 {
		dst.CompactForceRatio = src.CompactForceRatio
	}
	if src.SoftCompactRatio != 0 {
		dst.SoftCompactRatio = src.SoftCompactRatio
	}
	if src.MaxSubagentDepth != 0 {
		dst.MaxSubagentDepth = src.MaxSubagentDepth
	}
	if src.MaxSubagentConcurrency != 0 {
		dst.MaxSubagentConcurrency = src.MaxSubagentConcurrency
	}
	if src.MaxParallelWriters != 0 {
		dst.MaxParallelWriters = src.MaxParallelWriters
	}
	if src.MaxSteps != 0 {
		dst.MaxSteps = src.MaxSteps
	}
	if src.PlannerModel != "" {
		dst.PlannerModel = src.PlannerModel
	}
	if src.SubagentModel != "" {
		dst.SubagentModel = src.SubagentModel
	}
}

func mergeSandbox(dst, src *SandboxConfig) {
	if src.WorkspaceRoot != "" {
		dst.WorkspaceRoot = src.WorkspaceRoot
	}
	if len(src.AllowWrite) > 0 {
		dst.AllowWrite = src.AllowWrite
	}
	if len(src.ForbidRead) > 0 {
		dst.ForbidRead = src.ForbidRead
	}
	if len(src.ForbidWrite) > 0 {
		dst.ForbidWrite = src.ForbidWrite
	}
	if src.Bash != "" {
		dst.Bash = src.Bash
	}
	if src.Network {
		dst.Network = true
	}
}

func mergeSkills(dst, src *SkillsConfig) {
	if len(src.Paths) > 0 {
		dst.Paths = src.Paths
	}
	if len(src.ExcludedPaths) > 0 {
		dst.ExcludedPaths = src.ExcludedPaths
	}
	if len(src.Disabled) > 0 {
		dst.Disabled = src.Disabled
	}
}

func mergePermissions(dst, src *PermissionsConfig) {
	if len(src.Allow.Tools) > 0 {
		dst.Allow.Tools = src.Allow.Tools
	}
	if len(src.Allow.BashPattern) > 0 {
		dst.Allow.BashPattern = src.Allow.BashPattern
	}
	if len(src.Deny.BashPattern) > 0 {
		dst.Deny.BashPattern = src.Deny.BashPattern
	}
	if len(src.Deny.ForbidWrite) > 0 {
		dst.Deny.ForbidWrite = src.Deny.ForbidWrite
	}
}
