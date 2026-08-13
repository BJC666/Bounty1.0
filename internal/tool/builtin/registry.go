package builtin

import (
	"context"
	"os/exec"
	"time"

	"bounty/internal/devet"
	"bounty/internal/tool"
)

type ToolOptions struct {
	BashTimeout      time.Duration
	ProjectRoot      string
	DockerBashRunner func(ctx context.Context, command string) (string, error)
	SandboxFunc      func(*exec.Cmd) *exec.Cmd
}

func RegisterAll(reg *tool.Registry, opts ToolOptions) {
	reg.Add(&BashTool{
		Timeout:      opts.BashTimeout,
		DockerRunner: opts.DockerBashRunner,
		Sandbox:      opts.SandboxFunc,
	})
	reg.Add(&ReadFileTool{})
	reg.Add(&WriteFileTool{})
	reg.Add(&EditFileTool{})
	reg.Add(&GrepTool{})
	reg.Add(&GlobTool{})
	reg.Add(&WebFetchTool{})
	reg.Add(&WebSearchTool{})
	reg.Add(&CodeIndexTool{})
	reg.Add(&RememberTool{ProjectRoot: opts.ProjectRoot})
	reg.Add(&MemorySearchTool{ProjectRoot: opts.ProjectRoot})
	reg.Add(&BrowserTool{})
}

// RegisterDeVET registers all 5 DeVET tools wired to the given backend.
// Safe to call even if backend is nil — it will be a no-op.
func RegisterDeVET(reg *tool.Registry, backend *devet.Backend) {
	if backend == nil {
		return
	}
	dt := NewDeVETTools(backend)
	for _, t := range dt.All() {
		reg.Add(t)
	}
}
