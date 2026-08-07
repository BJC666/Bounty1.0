package permission

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bounty/internal/config"
)

type fakeTool struct{ name string }

func (f fakeTool) Name() string                                                      { return f.name }
func (f fakeTool) Description() string                                               { return "test tool" }
func (f fakeTool) Schema() json.RawMessage                                           { return json.RawMessage(`{}`) }
func (f fakeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) { return "", nil }
func (f fakeTool) ReadOnly() bool                                                    { return true }

func testGate() *Gate {
	return NewGate(
		config.PermissionsConfig{
			Deny: config.DenyConfig{ForbidWrite: []string{
				"Windows/*", "Program Files/*", "System32/*", "~/.ssh/*", "/etc/*",
			}},
		},
		config.SandboxConfig{
			ForbidRead: []string{"~/.ssh/*", ".env"},
		},
		PostureAuto,
	)
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestMatchesPolicyRelativeWindows(t *testing.T) {
	root := `C:\`
	if filepath.Separator != '\\' {
		root = "/"
	}
	path := filepath.Join(root, "Windows", "System32", "config", "SAM")
	if !matchesPolicy("Windows/*", path) {
		t.Errorf("matchesPolicy(Windows/*, %q) = false, want true", path)
	}
	if !matchesPolicy("System32/*", path) {
		t.Errorf("matchesPolicy(System32/*, %q) = false, want true", path)
	}
	ok := filepath.Join(root, "Program Files (x86)", "app", "bin.exe")
	if !matchesPolicy("Program Files (x86)/*", ok) {
		t.Errorf("matchesPolicy(Program Files (x86)/*, %q) = false, want true", ok)
	}
}

func TestMatchesPolicyHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	key := filepath.Join(home, ".ssh", "id_rsa")
	if !matchesPolicy("~/.ssh/*", key) {
		t.Errorf("matchesPolicy(~/.ssh/*, %q) = false, want true", key)
	}
}

func TestMatchesPolicyAbsolute(t *testing.T) {
	p := filepath.FromSlash("/etc/passwd")
	if !matchesPolicy(filepath.FromSlash("/etc/*"), p) {
		t.Errorf("matchesPolicy(/etc/*, %q) = false, want true", p)
	}
}

func TestMatchesPolicyNoFalsePositive(t *testing.T) {
	root := `C:\`
	if filepath.Separator != '\\' {
		root = "/"
	}
	path := filepath.Join(root, "work", "project", "main.go")
	for _, pat := range []string{"Windows/*", "~/.ssh/*", "/etc/*"} {
		if matchesPolicy(pat, path) {
			t.Errorf("matchesPolicy(%q, %q) = true, want false", pat, path)
		}
	}
}

func TestGateForbidWriteAbsolute(t *testing.T) {
	g := testGate()
	root := `C:\`
	if filepath.Separator != '\\' {
		root = "/"
	}
	args := mustJSON(t, map[string]string{"file_path": filepath.Join(root, "Windows", "System32", "evil.exe")})
	dec, err := g.Check(context.Background(), fakeTool{name: "write_file"}, args)
	if err == nil || dec != Deny {
		t.Errorf("write to System32: dec=%v err=%v, want Deny with error", dec, err)
	}
}

func TestGateForbidWriteHome(t *testing.T) {
	g := testGate()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	args := mustJSON(t, map[string]string{"file_path": filepath.Join(home, ".ssh", "authorized_keys")})
	dec, err := g.Check(context.Background(), fakeTool{name: "edit_file"}, args)
	if err == nil || dec != Deny {
		t.Errorf("write to ~/.ssh: dec=%v err=%v, want Deny with error", dec, err)
	}
}

func TestGateForbidWriteAllowedPath(t *testing.T) {
	g := testGate()
	root := `C:\`
	if filepath.Separator != '\\' {
		root = "/"
	}
	args := mustJSON(t, map[string]string{"file_path": filepath.Join(root, "work", "project", "main.go")})
	dec, err := g.Check(context.Background(), fakeTool{name: "write_file"}, args)
	if err != nil || dec != Allow {
		t.Errorf("write to workspace: dec=%v err=%v, want Allow", dec, err)
	}
}

func TestGateForbidRead(t *testing.T) {
	g := testGate()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	cases := []struct {
		tool string
		args map[string]string
	}{
		{"read_file", map[string]string{"file_path": filepath.Join(home, ".ssh", "id_rsa")}},
		{"grep", map[string]string{"path": filepath.Join(home, ".ssh")}},
		{"glob", map[string]string{"path": filepath.Join(home, ".ssh")}},
		{"code_index", map[string]string{"path": filepath.Join(home, ".ssh")}},
	}
	for _, c := range cases {
		dec, err := g.Check(context.Background(), fakeTool{name: c.tool}, mustJSON(t, c.args))
		if err == nil || dec != Deny {
			t.Errorf("%s on ~/.ssh: dec=%v err=%v, want Deny with error", c.tool, dec, err)
		}
	}
}

func TestGateForbidReadDotEnv(t *testing.T) {
	g := testGate()
	root := `C:\`
	if filepath.Separator != '\\' {
		root = "/"
	}
	env := filepath.Join(root, "work", "project", ".env")
	args := mustJSON(t, map[string]string{"file_path": env})
	dec, err := g.Check(context.Background(), fakeTool{name: "read_file"}, args)
	if err == nil || dec != Deny {
		t.Errorf("read of .env: dec=%v err=%v, want Deny with error", dec, err)
	}
}
