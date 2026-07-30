package builtin

import (
	"context"
	"os/exec"
	"time"

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
	reg.Add(&TodoWriteTool{})
	reg.Add(&WebFetchTool{})
	reg.Add(&WebSearchTool{})
	reg.Add(&CodeIndexTool{})
	reg.Add(&RememberTool{ProjectRoot: opts.ProjectRoot})
	reg.Add(&BrowserTool{})

	// DeVET integration tools (call DeVET REST API at localhost:8765)
	reg.Add(&DeVETHealthTool{})
	reg.Add(&DeVETBuildScenarioTool{})
	reg.Add(&DeVETVerifyChainTool{})
	reg.Add(&DeVETListAttacksTool{})
	reg.Add(&DeVETSimulateAttackTool{})
}
