package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsHasProvider(t *testing.T) {
	cfg := Defaults()
	if cfg.DefaultModel == "" {
		t.Error("DefaultModel should not be empty")
	}
	if cfg.Agent.MaxSteps != 50 {
		t.Error("MaxSteps should be 50")
	}
	if len(cfg.Permissions.Deny.BashPattern) == 0 {
		t.Error("should have deny patterns")
	}
}

func TestLoadEmptyDir(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
}

func TestLoadWithProjectConfig(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bounty.toml"), []byte(`
config_version = 1
default_model = "test/model"
[[providers]]
name = "test"
kind = "openai"
api_key_env = "TEST_KEY"
`), 0644)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "test/model" {
		t.Errorf("expected test/model, got %s", cfg.DefaultModel)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}
}

func TestValidate(t *testing.T) {
	cfg := Defaults()
	err := cfg.Validate()
	if err == nil {
		t.Error("should fail: no providers in defaults")
	}
}

func TestMergeConfigFieldLevel(t *testing.T) {
	dst := Defaults()
	// dst has existing values that must survive when src is sparse.
	dst.Agent.Temperature = 0.5
	dst.Sandbox.WorkspaceRoot = "/keep/root"

	src := &Config{
		Language: "zh",
		Agent: AgentConfig{
			MaxSteps: 99,
		},
		Sandbox: SandboxConfig{
			Bash: "pwsh",
		},
		Skills: SkillsConfig{
			ExcludedPaths: []string{"vendor"},
		},
		Permissions: PermissionsConfig{
			Deny: DenyConfig{ForbidWrite: []string{"/etc"}},
		},
		Hooks: HooksConfig{
			Enabled: true,
			Shell:   []HookConfig{{Event: "PreToolUse", Matcher: "bash", Command: "echo hi"}},
		},
	}
	mergeConfig(dst, src)

	if dst.Language != "zh" {
		t.Errorf("Language not merged: %q", dst.Language)
	}
	if dst.Agent.MaxSteps != 99 {
		t.Errorf("Agent.MaxSteps not merged: %d", dst.Agent.MaxSteps)
	}
	if dst.Agent.Temperature != 0.5 {
		t.Errorf("Agent.Temperature should survive: %v", dst.Agent.Temperature)
	}
	if dst.Sandbox.Bash != "pwsh" {
		t.Errorf("Sandbox.Bash not merged: %q", dst.Sandbox.Bash)
	}
	if dst.Sandbox.WorkspaceRoot != "/keep/root" {
		t.Errorf("Sandbox.WorkspaceRoot should survive: %q", dst.Sandbox.WorkspaceRoot)
	}
	if len(dst.Skills.ExcludedPaths) != 1 || dst.Skills.ExcludedPaths[0] != "vendor" {
		t.Errorf("Skills.ExcludedPaths not merged: %v", dst.Skills.ExcludedPaths)
	}
	if len(dst.Permissions.Deny.ForbidWrite) != 1 || dst.Permissions.Deny.ForbidWrite[0] != "/etc" {
		t.Errorf("Permissions.Deny.ForbidWrite not merged: %v", dst.Permissions.Deny.ForbidWrite)
	}
	if !dst.Hooks.Enabled {
		t.Error("Hooks.Enabled not merged")
	}
	if len(dst.Hooks.Shell) != 1 {
		t.Errorf("Hooks.Shell not merged: %d entries", len(dst.Hooks.Shell))
	}
}
