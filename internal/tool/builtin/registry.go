package builtin

import (
	"context"
	"time"

	"bounty/internal/tool"
)

type ToolOptions struct {
	BashTimeout     time.Duration
	ProjectRoot     string
	DockerBashRunner func(ctx context.Context, command string) (string, error)
}

func RegisterAll(reg *tool.Registry, opts ToolOptions) {
	reg.Add(&BashTool{
		Timeout:      opts.BashTimeout,
		DockerRunner: opts.DockerBashRunner,
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
}
