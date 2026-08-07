package guardian

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeTool struct{ name string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "fake" }
func (f fakeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (f fakeTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (f fakeTool) ReadOnly() bool                                           { return false }

func review(name, args string) (bool, string) {
	s := New(true)
	return s.Review(context.Background(), fakeTool{name: name}, json.RawMessage(args))
}

func TestReviewDisabledAlwaysAllows(t *testing.T) {
	s := New(false)
	ok, _ := s.Review(context.Background(), fakeTool{name: "bash"},
		json.RawMessage(`{"command":"rm -rf /"}`))
	if !ok {
		t.Fatal("disabled guardian must allow everything")
	}
}

func TestReviewDangerousBashCommands(t *testing.T) {
	cases := []string{
		`{"command":"rm -rf /etc"}`,
		`{"command":"sudo apt install x"}`,
		`{"command":"shutdown now"}`,
		`{"command":"reboot"}`,
		`{"command":"format /dev/sda1"}`,
		`{"command":"RM -rf /"}`,
		`{"command":"  rm -rf /tmp/x  "}`,
	}
	for _, args := range cases {
		if ok, reason := review("bash", args); ok {
			t.Errorf("expected block for %s, got allow (%s)", args, reason)
		}
	}
}

func TestReviewSafeBashCommands(t *testing.T) {
	cases := []string{
		`{"command":"ls -la"}`,
		`{"command":"go test ./..."}`,
		`{"command":"echo hello"}`,
		`{"command":"grep -r foo ."}`,
	}
	for _, args := range cases {
		if ok, reason := review("bash", args); !ok {
			t.Errorf("expected allow for %s, got block (%s)", args, reason)
		}
	}
}

func TestReviewSensitiveFilePaths(t *testing.T) {
	for _, name := range []string{"write_file", "edit_file"} {
		for _, args := range []string{
			`{"file_path":"/app/.env"}`,
			`{"path":"C:\\Users\\me\\.ssh\\id_rsa"}`,
			`{"file_path":".env.local"}`,
		} {
			if ok, _ := review(name, args); ok {
				t.Errorf("%s should block sensitive path %s", name, args)
			}
		}
	}
}

func TestReviewSafeFilePaths(t *testing.T) {
	for _, name := range []string{"write_file", "edit_file"} {
		for _, args := range []string{
			`{"file_path":"/app/main.go"}`,
			`{"path":"C:\\repo\\src\\app.ts"}`,
			`{"file_path":".gitignore"}`,
		} {
			if ok, reason := review(name, args); !ok {
				t.Errorf("%s should allow %s, got block (%s)", name, args, reason)
			}
		}
	}
}

func TestReviewUnknownToolsAlwaysAllow(t *testing.T) {
	if ok, _ := review("read_file", `{"path":"/etc/passwd"}`); !ok {
		t.Fatal("guardian must not block tools outside its review set")
	}
}

func TestReviewMalformedJSONAllows(t *testing.T) {
	if ok, _ := review("bash", `{"command":`); !ok {
		t.Fatal("malformed args must not panic or block")
	}
	if ok, _ := review("write_file", `not-json`); !ok {
		t.Fatal("malformed args must not block write_file")
	}
}

func TestExtractCommandMissingField(t *testing.T) {
	if _, ok := extractCommand(json.RawMessage(`{"path":"/x"}`)); ok {
		t.Fatal("extractCommand should fail when command missing")
	}
}

func TestExtractPathPrefersFilePath(t *testing.T) {
	got, ok := extractPath(json.RawMessage(`{"file_path":"/a","path":"/b"}`))
	if !ok || got != "/a" {
		t.Fatalf("got %q ok=%v, want /a true", got, ok)
	}
}
