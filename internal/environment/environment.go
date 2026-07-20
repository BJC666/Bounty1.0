package environment

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Info holds the results of environment probing.
type Info struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Shell    string `json:"shell"`
	Go       string `json:"go,omitempty"`
	Git      string `json:"git,omitempty"`
	Node     string `json:"node,omitempty"`
	Python   string `json:"python,omitempty"`
	Rust     string `json:"rust,omitempty"`
	Docker   string `json:"docker,omitempty"`
	HomeDir  string `json:"home_dir"`
	Hostname string `json:"hostname"`
}

var (
	cache     *Info
	cacheOnce sync.Once
)

// Probe runs environment detection once and caches the result.
func Probe() *Info {
	cacheOnce.Do(func() {
		cache = probe()
	})
	return cache
}

func probe() *Info {
	info := &Info{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	if home, err := os.UserHomeDir(); err == nil {
		info.HomeDir = home
	}
	if host, err := os.Hostname(); err == nil {
		info.Hostname = host
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		info.Shell = shell
	} else if comspec := os.Getenv("COMSPEC"); comspec != "" {
		info.Shell = comspec
	}

	// Probe tool versions (best-effort, ignore errors)
	info.Go = probeCmd("go", "version")
	info.Git = probeCmd("git", "--version")
	info.Node = probeCmd("node", "--version")
	info.Python = probeCmd("python3", "--version")
	if info.Python == "" {
		info.Python = probeCmd("python", "--version")
	}
	info.Rust = probeCmd("rustc", "--version")
	info.Docker = probeCmd("docker", "--version")

	return info
}

func probeCmd(cmd string, args ...string) string {
	c := exec.Command(cmd, args...)
	out, err := c.Output()
	if err != nil {
		return ""
	}
	result := strings.TrimSpace(string(out))
	if len(result) > 100 {
		result = result[:100]
	}
	return result
}

// Block returns the cache-stable environment section for the system prompt.
func (i *Info) Block() string {
	var sb strings.Builder
	sb.WriteString("## Environment\n")
	sb.WriteString(fmt.Sprintf("- OS: %s/%s\n", i.OS, i.Arch))
	if i.Shell != "" {
		sb.WriteString(fmt.Sprintf("- Shell: %s\n", i.Shell))
	}
	if i.Go != "" {
		sb.WriteString(fmt.Sprintf("- Go: %s\n", i.Go))
	}
	if i.Git != "" {
		sb.WriteString(fmt.Sprintf("- Git: %s\n", i.Git))
	}
	if i.Node != "" {
		sb.WriteString(fmt.Sprintf("- Node: %s\n", i.Node))
	}
	if i.Python != "" {
		sb.WriteString(fmt.Sprintf("- Python: %s\n", i.Python))
	}
	if i.Rust != "" {
		sb.WriteString(fmt.Sprintf("- Rust: %s\n", i.Rust))
	}
	if i.Docker != "" {
		sb.WriteString(fmt.Sprintf("- Docker: %s\n", i.Docker))
	}
	sb.WriteString(fmt.Sprintf("- Home: %s\n", i.HomeDir))
	return sb.String()
}

// Invalidate clears the cache (call after tool installations).
func Invalidate() {
	cacheOnce = sync.Once{}
	cache = nil
}
