package config

import "fmt"

type Config struct {
	Version      int               `toml:"config_version"`
	DefaultModel string            `toml:"default_model"`
	Language     string            `toml:"language"`
	Providers    []ProviderConfig  `toml:"providers"`
	Agent        AgentConfig       `toml:"agent"`
	Sandbox      SandboxConfig     `toml:"sandbox"`
	Skills       SkillsConfig      `toml:"skills"`
	Plugins      []PluginEntry     `toml:"plugins"`
	Permissions  PermissionsConfig `toml:"permissions"`
	Hooks        HooksConfig       `toml:"hooks"`
}

type ProviderConfig struct {
	Name          string            `toml:"name"`
	Kind          string            `toml:"kind"`
	BaseURL       string            `toml:"base_url"`
	Models        []string          `toml:"models"`
	APIKeyEnv     string            `toml:"api_key_env"`
	ContextWindow int               `toml:"context_window"`
	Effort        string            `toml:"effort"`
	Extra         map[string]string `toml:"extra"`
}

type AgentConfig struct {
	Temperature            float64 `toml:"temperature"`
	CompactRatio           float64 `toml:"compact_ratio"`
	CompactForceRatio      float64 `toml:"compact_force_ratio"`
	SoftCompactRatio       float64 `toml:"soft_compact_ratio"`
	MaxSubagentDepth       int     `toml:"max_subagent_depth"`
	MaxSubagentConcurrency int     `toml:"max_subagent_concurrency"`
	MaxParallelWriters     int     `toml:"max_parallel_writers"`
	MaxSteps               int     `toml:"max_steps"`
	PlannerModel           string  `toml:"planner_model"`
	SubagentModel          string  `toml:"subagent_model"`
}

type SandboxConfig struct {
	WorkspaceRoot string   `toml:"workspace_root"`
	AllowWrite    []string `toml:"allow_write"`
	ForbidRead    []string `toml:"forbid_read"`
	ForbidWrite   []string `toml:"forbid_write"`
	Bash          string   `toml:"bash"`
	Network       bool     `toml:"network"`
}

type SkillsConfig struct {
	Paths         []string `toml:"paths"`
	ExcludedPaths []string `toml:"excluded_paths"`
	Disabled      []string `toml:"disabled_skills"`
}

type PluginEntry struct {
	Name     string   `toml:"name"`
	Command  string   `toml:"command"`
	Args     []string `toml:"args"`
	Env      []string `toml:"env"`
	URL      string   `toml:"url"`
	ReadOnly bool     `toml:"read_only"`
	Trust    bool     `toml:"trust"`
}

type PermissionsConfig struct {
	Allow AllowConfig `toml:"allow"`
	Deny  DenyConfig  `toml:"deny"`
}

type AllowConfig struct {
	Tools       []string `toml:"tools"`
	BashPattern []string `toml:"bash_patterns"`
}

type DenyConfig struct {
	BashPattern []string `toml:"bash_patterns"`
	ForbidWrite []string `toml:"forbid_write"`
}

type HooksConfig struct {
	Enabled bool         `toml:"enabled"`
	Shell   []HookConfig `toml:"shell"`
}

type HookConfig struct {
	Event   string `toml:"event"`
	Matcher string `toml:"matcher"`
	Command string `toml:"command"`
	Timeout int    `toml:"timeout_seconds"`
}

func (c *Config) Validate() error {
	if c.DefaultModel == "" {
		return fmt.Errorf("default_model is required")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	return nil
}
