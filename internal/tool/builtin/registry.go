package builtin

import (
	"time"

	"bounty/internal/tool"
)

type ToolOptions struct {
	BashTimeout time.Duration
	ProjectRoot string
}

func RegisterAll(reg *tool.Registry, opts ToolOptions) {
	reg.Add(&BashTool{Timeout: opts.BashTimeout})
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
}
