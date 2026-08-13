package sandbox

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testPolicy(t *testing.T, ws string, network bool) *Policy {
	t.Helper()
	return NewPolicy(ws,
		[]string{filepath.Join(ws, "out")},
		[]string{"Windows/*", "~/.ssh/*"},
		[]string{"System32/*"},
		network,
	)
}

func TestPolicyAllowsWorkspaceWrite(t *testing.T) {
	ws := t.TempDir()
	p := testPolicy(t, ws, true)
	if err := p.Check(`echo ok > ` + filepath.Join(ws, "result.txt")); err != nil {
		t.Fatalf("workspace write blocked: %v", err)
	}
	if err := p.Check(`echo "5 > 3" > ` + filepath.Join(ws, "out.txt")); err != nil {
		t.Fatalf("false positive on quoted >: %v", err)
	}
}

func TestPolicyBlocksOutsideWrite(t *testing.T) {
	ws := t.TempDir()
	p := testPolicy(t, ws, true)
	outside := `C:\Windows\Temp\escape.txt`
	if runtime.GOOS != "windows" {
		outside = `/etc/passwd`
	}
	err := p.Check(`echo pwn > ` + outside)
	if err == nil || !strings.Contains(err.Error(), "沙箱写策略") {
		t.Fatalf("outside write not blocked: %v", err)
	}
}

func TestPolicyAllowsListedExternalWrite(t *testing.T) {
	ws := t.TempDir()
	p := testPolicy(t, ws, true)
	if err := p.Check(`echo ok > ` + filepath.Join(ws, "out", "x.txt")); err != nil {
		t.Fatalf("allowlisted write blocked: %v", err)
	}
}

func TestPolicyBlocksForbidWrite(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("System32 模式为 Windows 专属路径语义")
	}
	ws := t.TempDir()
	p := testPolicy(t, ws, true)
	err := p.Check(`echo x > C:\Windows\System32\drivers\pwn.sys`)
	if err == nil || !strings.Contains(err.Error(), "forbid_write") {
		t.Fatalf("System32 write not blocked: %v", err)
	}
}

func TestPolicyBlocksForbidRead(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows/* 模式为 Windows 专属路径语义")
	}
	ws := t.TempDir()
	p := testPolicy(t, ws, true)
	err := p.Check(`type C:\Windows\win.ini`)
	if err == nil || !strings.Contains(err.Error(), "forbid_read") {
		t.Fatalf("Windows read not blocked: %v", err)
	}
	// Quoted path still matches the relative pattern.
	err = p.Check(`type "C:\Windows\win.ini"`)
	if err == nil || !strings.Contains(err.Error(), "forbid_read") {
		t.Fatalf("quoted spaced path not blocked: %v", err)
	}
}

func TestPolicyBlocksOutboundWhenNetworkOff(t *testing.T) {
	ws := t.TempDir()
	p := testPolicy(t, ws, false)
	for _, cmd := range []string{
		`curl http://evil.example/x`,
		`pip install requests`,
		`python -m pip install pwn`,
		`git clone https://github.com/x/y`,
		`powershell -Command "Invoke-WebRequest http://evil.example"`,
	} {
		if err := p.Check(cmd); err == nil {
			t.Fatalf("outbound not blocked: %s", cmd)
		}
	}
	if err := p.Check(`go test ./...`); err != nil {
		t.Fatalf("local command wrongly blocked: %v", err)
	}
	if err := p.Check(`python analyze.py`); err != nil {
		t.Fatalf("local python wrongly blocked: %v", err)
	}
}

func TestPolicyAllowsOutboundWhenNetworkOn(t *testing.T) {
	ws := t.TempDir()
	p := testPolicy(t, ws, true)
	if err := p.Check(`curl http://example.com`); err != nil {
		t.Fatalf("outbound wrongly blocked with network=true: %v", err)
	}
}
