package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bounty/internal/agent"
	"bounty/internal/channel"
	"bounty/internal/channel/httpapi"
	"bounty/internal/channel/terminal"
	"bounty/internal/channel/webhook"
	"bounty/internal/checkpoint"
	"bounty/internal/config"
	"bounty/internal/control"
	"bounty/internal/devet"
	"bounty/internal/environment"
	"bounty/internal/event"
	"bounty/internal/guardian"
	"bounty/internal/hook"
	"bounty/internal/mcp"
	"bounty/internal/memory"
	"bounty/internal/permission"
	"bounty/internal/plugin"
	"bounty/internal/provider"
	"bounty/internal/provider/anthropic"
	"bounty/internal/provider/ollama"
	"bounty/internal/provider/openai"
	"bounty/internal/provider/openai_native"
	"bounty/internal/repomap"
	"bounty/internal/sandbox"
	"bounty/internal/secrets"
	"bounty/internal/skill"
	"bounty/internal/store"
	"bounty/internal/tool"
	"bounty/internal/tool/builtin"
)

// Options carries the runtime overrides and dependencies that a call site
// supplies when building a Controller.
type Options struct {
	Model     string
	MaxSteps  int
	Sink      event.Sink
	Posture   permission.Posture
	SessionID string
	Asker     agent.Asker // interactive approval prompts (nil = deny on Ask)
}

// Build is the one-stop assembly function. It wires together every subsystem
// (config, secrets, provider, tools, memory, skills, permissions, hooks, agent,
// store) and returns a ready-to-use Controller. Build is deliberately
// self-contained so that any frontend (CLI, HTTP, WebSocket) can call it with
// the same incantation.
func Build(cfg *config.Config, opts Options) (*control.Controller, error) {
	// 1. Resolve model (provider/model)
	provName, modelName := parseModel(cfg.DefaultModel)
	if opts.Model != "" {
		provName, modelName = parseModel(opts.Model)
	}

	// 2. Find provider config
	var provCfg *config.ProviderConfig
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == provName {
			provCfg = &cfg.Providers[i]
			break
		}
	}
	if provCfg == nil {
		return nil, fmt.Errorf("provider %q not found", provName)
	}

	// 3. Load API key pool (not needed for ollama)
	var apiKey string
	if provCfg.Kind != "ollama" {
		keyPool, err := secrets.NewPool(provCfg.APIKeyEnv)
		if err != nil {
			return nil, fmt.Errorf("api key for %s: %w", provName, err)
		}
		apiKey, err = keyPool.Get()
		if err != nil {
			return nil, fmt.Errorf("api key for %s: %w", provName, err)
		}
	}

	// 4. Create Provider
	prov, err := BuildProvider(provCfg.Kind, provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)
	if err != nil {
		return nil, err
	}

	// 5. Create tool registry + register builtins
	reg := tool.NewRegistry()

	// 5a. If Docker is available, set up container sandbox for bash tool.
	var dockerRunner func(ctx context.Context, command string) (string, error)
	if sandbox.Available() {
		ds := sandbox.NewDockerSandbox("alpine:3.21", cfg.Sandbox.WorkspaceRoot)
		dockerRunner = ds.Run
	}

	repoMapMgr := repomap.NewManager(workspaceRootFor(cfg))

	sandboxPolicy := sandbox.NewPolicy(
		cfg.Sandbox.WorkspaceRoot,
		cfg.Sandbox.AllowWrite,
		cfg.Sandbox.ForbidRead,
		cfg.Sandbox.ForbidWrite,
		cfg.Sandbox.Network,
	)
	jobOpts := sandbox.JobOptions{
		WorkspaceRoot: cfg.Sandbox.WorkspaceRoot,
		AllowWrite:    cfg.Sandbox.AllowWrite,
		ForbidRead:    cfg.Sandbox.ForbidRead,
		ForbidWrite:   cfg.Sandbox.ForbidWrite,
		Network:       cfg.Sandbox.Network,
	}
	builtin.RegisterAll(reg, builtin.ToolOptions{
		BashTimeout:      120e9,
		ProjectRoot:      cfg.Sandbox.WorkspaceRoot,
		DockerBashRunner: dockerRunner,
		SandboxFunc:      func(cmd *exec.Cmd) *exec.Cmd { return sandbox.Wrap(cmd, cfg.Sandbox.WorkspaceRoot) },
		BashPolicy:       sandboxPolicy.Check,
		BashSandboxStart: func(cmd *exec.Cmd) (*sandbox.Container, error) { return sandbox.StartContained(cmd, jobOpts) },
	})

	// 5a2. Start or connect to DeVET backend (optional — tools unavailable if not found)
	devetBackend, devetErr := devet.StartOrConnect(context.Background(), filepath.Join("..", "DeVET"))
	if devetErr != nil {
		fmt.Fprintf(os.Stderr, "DeVET: %v (tools will be unavailable)\n", devetErr)
	}
	builtin.RegisterDeVET(reg, devetBackend)
	// P4-1: full-chain integration — every task/fleet sub-agent result is
	// mirrored into DeVET and auto-verified; the client also keeps the
	// latest snapshot for the web chain-visualisation panel.
	var devetMirror *devet.MirrorClient
	if devetBackend != nil {
		// Multi-tenant: every Bounty session gets its own DeVET tenant so
		// concurrent sessions do not overwrite each other's chains.
		devetMirror = devet.NewMirrorClient(devetBackend, opts.SessionID)
	}
	reg.Add(builtin.NewRepoMapTool(repoMapMgr))

	// 5b. Connect MCP plugins (bounty.toml [plugins]) and JSON config files
	// (user-level bounty-data/mcp.json + project-level .bounty/mcp.json).
	mcpHost := mcp.NewHost()
	for _, p := range cfg.Plugins {
		spec := mcp.Spec{
			Name:     p.Name,
			Command:  p.Command,
			Args:     p.Args,
			Env:      p.Env,
			URL:      p.URL,
			ReadOnly: p.ReadOnly,
			Trust:    p.Trust,
		}
		if err := mcpHost.Connect(spec); err != nil {
			// Log warning but continue — MCP servers are optional
			fmt.Fprintf(os.Stderr, "Warning: MCP server %q: %v\n", p.Name, err)
		}
	}
	ws := cfg.Sandbox.WorkspaceRoot
	jsonSpecs, err := mcp.LoadSpecs(
		filepath.Join(ws, "bounty-data", "mcp.json"),
		filepath.Join(ws, ".bounty", "mcp.json"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP config files: %v\n", err)
	}
	for _, spec := range jsonSpecs {
		if err := mcpHost.Connect(spec); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: MCP server %q: %v\n", spec.Name, err)
		}
	}
	mcpHost.RegisterTools(reg)

	// 6. Load memory
	memDocs, _ := memory.Load(cfg.Sandbox.WorkspaceRoot)

	// 7. Load skills — P4-2: the repo-bundled skills/ directory and the
	// user data dir are always discovered first, then configured paths;
	// dangerous skill files are refused by the safety audit and reported.
	skillStore := skill.NewStore()
	skillPaths := append([]string{
		filepath.Join("skills"),
		filepath.Join(dataDir(), "skills"),
	}, cfg.Skills.Paths...)
	skillStore.Discover(skillPaths)
	skillStore.Disable(cfg.Skills.Disabled)
	for _, rejected := range skillStore.Rejected {
		fmt.Fprintf(os.Stderr, "Skill audit: rejected %q (%s): %v\n",
			rejected.Name, rejected.SourcePath, rejected.Findings)
	}

	// 7a. Skill curator — lifecycle management (active -> inactive -> archived)
	curator := skill.NewCurator(skill.CuratorConfig{Enabled: true}, skillStore, dataDir())

	// Run lifecycle check on startup
	report := curator.Run()
	if report != nil && len(report.Inactivated)+len(report.Archived) > 0 {
		fmt.Fprintf(os.Stderr, "Curator: %s\n", report.String())
	}

	// 7a-2. Learning graph — tracks relationships between skills, tools, and memory
	learningGraph := agent.NewLearningGraph(dataDir())

	// 7b. Load commands — project/user dirs plus plugins installed from the
	// marketplace (~/bounty-data/plugins/<name>/commands).
	cmdStore := plugin.NewCommandStore()
	cmdStore.Discover(append(pluginDirs(dataDir(), "commands"), []string{
		filepath.Join(cfg.Sandbox.WorkspaceRoot, ".bounty", "commands"),
		filepath.Join(dataDir(), "commands"),
	}...))

	// 7c. Load agent definitions — same marketplace plugin dirs.
	agentStore := plugin.NewAgentStore()
	agentStore.Discover(append(pluginDirs(dataDir(), "agents"), []string{
		filepath.Join(cfg.Sandbox.WorkspaceRoot, ".bounty", "agents"),
		filepath.Join(dataDir(), "agents"),
	}...))

	// 8. Build system prompt (repo map appended separately so the agent can
	// refresh it per turn without rebuilding the static base).
	basePrompt := buildSystemPrompt(cfg, modelName, memDocs, skillStore.Index(), cmdStore)
	systemPrompt := basePrompt
	if block := repoMapMgr.Render(); block != "" {
		systemPrompt = basePrompt + block
	}

	if opts.Posture == permission.PosturePlan {
		systemPrompt += planContractText()
	}

	// 9. Create Session
	session := agent.NewSession(systemPrompt)

	// 9b. Fanout sink — the agent, controller, and any dynamically attached
	// frontends (e.g. an SSE stream) all observe the same event stream.
	// Secrets are redacted at the fanout so no live stream (console, SSE,
	// TUI) can leak API keys or private keys (CoT-leakage defense).
	fanout := event.NewFanout()
	fanout.Redact = memory.RedactSensitive
	if opts.Sink != nil {
		fanout.Add(opts.Sink)
	}

	// 10. Create permission gate
	permGate := permission.NewGate(cfg.Permissions, cfg.Sandbox, opts.Posture)

	// 10b. In yolo posture, wrap the gate with the guardian so sensitive
	// operations (dangerous bash, sensitive file writes) still escalate to
	// user approval instead of running unchecked.
	var agentGate agent.Gate = gateAdapter{gate: permGate}
	if opts.Posture == permission.PostureYolo {
		agentGate = guardianGate{gate: agentGate, guardian: guardian.New(true)}
	}

	// 11. Create hook runner
	var hookRunner *hook.Runner
	if cfg.Hooks.Enabled {
		hookRunner = hook.NewRunner(convertHooks(cfg.Hooks.Shell))
	}

	// 12. Create Agent — wire in insights, background review, checkpoints,
	// and the approval asker so the subsystems advertised in the docs are
	// actually reachable.
	sessionInsights := agent.NewSessionInsights(opts.SessionID)
	reviewer := agent.NewBackgroundReviewer(agent.ReviewConfig{
		Enabled:  true,
		MaxWait:  8 * time.Second,
		MinTurns: 3,
	})
	var ckpt agent.Checkpointer
	if opts.SessionID != "" {
		ckptDir := filepath.Join(dataDir(), "checkpoints", opts.SessionID)
		// P3-3: prefer the git shadow repo (full-tree snapshot per user
		// message, tag msg-<N>); fall back to the legacy file snapshots when
		// git is missing or the workspace root is unusable.
		gitStore, err := checkpoint.NewGit(workspaceRootFor(cfg), ckptDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git checkpoints unavailable (%v); falling back to file snapshots\n", err)
			ckptStore, err2 := checkpoint.New(ckptDir)
			if err2 != nil {
				fmt.Fprintf(os.Stderr, "Warning: checkpoints unavailable: %v\n", err2)
			} else {
				ckpt = ckptStore
			}
		} else {
			ckpt = gitStore
		}
	}
	todoSum := &todoSummary{}

	ag := agent.New(prov, reg, session, agent.Options{
		ProviderLabel: provName,
		DeVET:         devetMirror,
		MaxSteps:      opts.MaxSteps,
		Temperature:   cfg.Agent.Temperature,
		Sink:          fanout,
		Gate:          agentGate,
		Hooks:         hookAdapter{runner: hookRunner},
		Asker:         opts.Asker,
		Insights:      sessionInsights,
		Reviewer:      reviewer,
		Checkpointer:  ckpt,
		LearningGraph: learningGraph,
		Compact: &agent.CompactConfig{
			SoftRatio:  cfg.Agent.SoftCompactRatio,
			Ratio:      cfg.Agent.CompactRatio,
			ForceRatio: cfg.Agent.CompactForceRatio,
		},
		MemoryDir: workspaceRootFor(cfg),
		RepoMap:   repoMapMgr,
		Todos:     todoSum,
		// P3-4: sub-agents (task tool) may switch to a cheaper configured
		// model via "provider/model" or a bare model name (bare names resolve
		// against the single configured provider).
		ProvFactory: func(model string) (provider.Provider, error) {
			provName, modelName := parseModel(model)
			var subCfg *config.ProviderConfig
			for i := range cfg.Providers {
				if cfg.Providers[i].Name == provName {
					subCfg = &cfg.Providers[i]
					break
				}
			}
			if subCfg == nil && len(cfg.Providers) == 1 {
				subCfg = &cfg.Providers[0]
			}
			if subCfg == nil {
				return nil, fmt.Errorf("provider %q not found in config", provName)
			}
			var subKey string
			if subCfg.Kind != "ollama" {
				keyPool, err := secrets.NewPool(subCfg.APIKeyEnv)
				if err != nil {
					return nil, fmt.Errorf("api key for %s: %w", provName, err)
				}
				subKey, err = keyPool.Get()
				if err != nil {
					return nil, fmt.Errorf("api key for %s: %w", provName, err)
				}
			}
			return BuildProvider(subCfg.Kind, subCfg.BaseURL, subKey, modelName, subCfg.ContextWindow)
		},
	})

	// 12b. Register subagent tools onto the same registry
	reg.Add(agent.NewTaskTool(ag, cfg.Agent.MaxSubagentDepth))
	reg.Add(agent.NewReadOnlyTaskTool(ag, cfg.Agent.MaxSubagentDepth))
	reg.Add(agent.NewFleetTool(ag, cfg.Agent.MaxSubagentDepth, cfg.Agent.MaxParallelWriters))

	// 13. Open store
	st, err := store.New(filepath.Join(dataDir(), "bounty.db"))
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	reg.Add(&builtin.TodoWriteTool{Store: st, SessionID: opts.SessionID})
	todoSum.st = st
	todoSum.sessionID = opts.SessionID

	// 13b. Restore existing session messages if resuming
	sessionTitle := "New Session"
	if opts.SessionID != "" {
		if existing, loadErr := st.LoadSession(opts.SessionID); loadErr == nil {
			sessionTitle = existing.Title
			// Restore system prompt from saved session
			if existing.SystemPrompt != "" {
				session = agent.NewSession(existing.SystemPrompt)
			}
			// Load existing messages
			if msgs, loadErr := st.LoadMessages(opts.SessionID); loadErr == nil && len(msgs) > 0 {
				for _, m := range msgs {
					var toolCalls []provider.ToolCall
					if m.ToolCalls != "" {
						json.Unmarshal([]byte(m.ToolCalls), &toolCalls)
					}
					session.Add(provider.Message{
						Role:      m.Role,
						Content:   m.Content,
						ToolCalls: toolCalls,
						ToolName:  m.ToolName,
					})
				}
			}
			// Update the agent to use the restored session
			ag.SetSession(session)
		}
	}

	// 14. Save session
	if opts.SessionID != "" {
		st.SaveSession(&store.Session{
			ID: opts.SessionID, Title: sessionTitle,
			Model: modelName, Provider: provName, SystemPrompt: systemPrompt,
		})
	}

	// 15. Create Controller
	ctrl := control.New(ag, fanout, st, hookRunner, permGate, skillStore, cmdStore, agentStore, opts.SessionID)
	if devetMirror != nil {
		ctrl.AttachDeVET(devetMirror.State)
	}
	if r, ok := ckpt.(checkpoint.Restorer); ok {
		ctrl.SetCheckpointRestorer(r)
	}
	return ctrl, nil
}

// BuildProvider constructs a provider from explicit connection parameters. It is
// used by runtime model switching (e.g. the web console), where base URL and API
// key come from the user instead of the config file.
func BuildProvider(kind, baseURL, apiKey, model string, contextWindow int) (provider.Provider, error) {
	switch kind {
	case "openai":
		return openai.New(baseURL, apiKey, model, contextWindow), nil
	case "anthropic":
		return anthropic.New(baseURL, apiKey, model, contextWindow), nil
	case "ollama":
		p, err := ollama.New(baseURL, model)
		if err != nil {
			return nil, fmt.Errorf("ollama: %w", err)
		}
		return p, nil
	case "openai_native":
		return openai_native.New(apiKey, model, contextWindow), nil
	default:
		return nil, fmt.Errorf("unknown provider kind: %s", kind)
	}
}

// TestProvider sends a minimal chat request to verify that the endpoint and API
// key actually work before a runtime model switch is committed.
func TestProvider(ctx context.Context, p provider.Provider) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, []provider.Message{{Role: "user", Content: "Reply with OK"}}, nil, provider.StreamOpts{Temperature: 0})
	if err != nil {
		return err
	}
	for ev := range ch {
		if ev.Err != nil {
			return ev.Err
		}
		if ev.Done {
			return nil
		}
	}
	return fmt.Errorf("no response from provider")
}

// RebuildSession saves the current session and builds a fresh controller with a
// new session ID. This is used by session management (e.g., TUI /new and /switch
// commands) to transition between sessions without restarting the process.
func RebuildSession(ctrl *control.Controller, cfg *config.Config, sessionID string, sink event.Sink, asker agent.Asker) (*control.Controller, error) {
	if ctrl != nil {
		ctrl.SaveTurn()
	}
	opts := Options{MaxSteps: cfg.Agent.MaxSteps, Sink: sink, Posture: permission.PostureAuto, SessionID: sessionID, Asker: asker}
	return Build(cfg, opts)
}

// ---------------------------------------------------------------------------
// Channel support
// ---------------------------------------------------------------------------

// channelHandler bridges incoming channel messages to the controller.
type channelHandler struct {
	ctrl *control.Controller
}

func (h *channelHandler) HandleMessage(ctx context.Context, msg channel.Message) error {
	return h.ctrl.Send(ctx, msg.Text)
}

// NewChannelRegistry creates a channel registry wired to the given controller,
// and registers the default webhook channel.
func NewChannelRegistry(ctrl *control.Controller) *channel.Registry {
	reg := channel.NewRegistry(&channelHandler{ctrl: ctrl})
	reg.Register(webhook.New("webhook", "Webhook Receiver", reg.Handler()))
	reg.Register(terminal.New("terminal", reg.Handler()))
	reg.Register(httpapi.New("httpapi", reg.Handler(), 9090))
	return reg
}

// ---------------------------------------------------------------------------
// Adapters
// ---------------------------------------------------------------------------

// gateAdapter bridges *permission.Gate into the agent.Gate interface. The
// adapter is necessary because permission.Decision is a type alias (= int)
// while agent.Decision is a named type (int), and Go requires an exact
// signature match for interface satisfaction.
type gateAdapter struct {
	gate *permission.Gate
}

func (a gateAdapter) Check(ctx context.Context, t tool.Tool, args json.RawMessage) (agent.Decision, error) {
	dec, err := a.gate.Check(ctx, t, args)
	return agent.Decision(dec), err
}

// guardianGate wraps another gate with the YOLO-mode guardian. When the
// guardian flags a sensitive operation, the decision is escalated to Ask so
// it still requires user approval even in yolo posture.
type guardianGate struct {
	gate     agent.Gate
	guardian *guardian.Session
}

func (g guardianGate) Check(ctx context.Context, t tool.Tool, args json.RawMessage) (agent.Decision, error) {
	if g.guardian != nil {
		if proceed, _ := g.guardian.Review(ctx, t, args); !proceed {
			return agent.Ask, nil
		}
	}
	return g.gate.Check(ctx, t, args)
}

// hookAdapter bridges *hook.Runner into the agent.ToolHooks interface.
type hookAdapter struct {
	runner *hook.Runner
}

func (a hookAdapter) PreToolUse(ctx context.Context, name string, args json.RawMessage) error {
	if a.runner == nil {
		return nil
	}
	result, err := a.runner.Fire(ctx, hook.PreToolUse, hook.Payload{
		Event: hook.PreToolUse, ToolName: name, ToolInput: args,
	})
	if err != nil {
		return err
	}
	if !result.Continue {
		return fmt.Errorf("tool %s blocked by hook", name)
	}
	return nil
}

func (a hookAdapter) PreCompact(ctx context.Context, tokens int, dropped int) error {
	if a.runner == nil {
		return nil
	}
	_, err := a.runner.Fire(ctx, hook.PreCompact, hook.Payload{
		Event:  hook.PreCompact,
		Reason: fmt.Sprintf("context %d tokens, %d messages to summarize", tokens, dropped),
	})
	return err
}

func (a hookAdapter) PostToolUse(ctx context.Context, name string, result string, execErr error) {
	if a.runner == nil {
		return
	}
	payload := hook.Payload{Event: hook.PostToolUse, ToolName: name, ToolResult: result}
	if execErr != nil {
		payload.ToolErr = execErr.Error()
	}
	// fire-and-forget — hook errors must not break the agent loop
	a.runner.Fire(ctx, hook.PostToolUse, payload)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseModel splits a "provider/model" string into its two components.
// When no slash is present the same string is used for both.
func parseModel(full string) (provider, model string) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return full, full
}

// dataDir returns the platform-standard data directory for Bounty.
// pluginDirs returns <dataDir>/plugins/<name>/<sub> for every installed
// plugin directory that carries a plugin.toml manifest. Marketplace-installed
// plugins contribute commands/ and agents/ this way.
func pluginDirs(dataRoot, sub string) []string {
	root := filepath.Join(dataRoot, "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pluginRoot := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(pluginRoot, "plugin.toml")); err != nil {
			continue
		}
		out = append(out, filepath.Join(pluginRoot, sub))
	}
	return out
}

func dataDir() string {
	if dir := os.Getenv("BOUNTY_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "bounty-data")
}

// buildSystemPrompt constructs the system prompt by combining the base
// persona, workspace root, project memory documents, and available skills.
func buildSystemPrompt(cfg *config.Config, modelName string, docs []memory.Doc, skills []skill.IndexEntry, cmdStore *plugin.CommandStore) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are Bounty, a general-purpose AI agent running on **%s**. You help users with software engineering, research, data analysis, and automation tasks. If asked which model or provider you are using, answer with: %s\n", modelName, modelName))
	sb.WriteString("\n## Tool Usage Rules\n")
	sb.WriteString("- For casual conversation, greetings, and simple questions: respond directly WITHOUT using any tools.\n")
	sb.WriteString("- Use tools ONLY when you need file access, code search, web search, shell commands, or other specific capabilities.\n")
	sb.WriteString("- Never call web_search for greetings or small talk.\n")
	sb.WriteString("- If unsure whether to use a tool, default to answering directly.\n")
	sb.WriteString("- `memory_search` retrieves facts saved by `remember` (user preferences, conventions, lessons learned); prefer it over guessing when the user references earlier agreements.\n")
	sb.WriteString("- Before reading or editing a file, locate it with `glob` first unless you are certain of the exact path; guessing paths is the #1 avoidable tool failure.\n")
	sb.WriteString("\n## DeVET Security Tools\n")
	sb.WriteString("You have DeVET multi-agent delegation verification tools available:\n")
	sb.WriteString("- `devet_health` — check DeVET backend status\n")
	sb.WriteString("- `devet_build_scenario` — build a Trading DAO delegation chain\n")
	sb.WriteString("- `devet_verify_chain` — verify chain integrity with 7 recursive checks\n")
	sb.WriteString("- `devet_list_attacks` — list 8 attack types (all detected at 100%)\n")
	sb.WriteString("- `devet_simulate_attack` — simulate an attack and show blame attribution\n")
	sb.WriteString("When asked about DeVET, delegation chains, or multi-agent security, use the devet_* tools directly. Do NOT search the web for DeVET information.\n\n")

	// Environment info (cached — stable across turns)
	sb.WriteString(environment.Probe().Block())
	sb.WriteString("\n")

	workspaceRoot := cfg.Sandbox.WorkspaceRoot
	if workspaceRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			workspaceRoot = wd
		}
	}
	if workspaceRoot != "" {
		sb.WriteString("## Workspace\n" + workspaceRoot + "\n\n")
	}
	sb.WriteString("## Project Memory\n")
	for _, doc := range docs {
		if len(doc.InjectionHits) > 0 {
			// Suspicious memory is rendered inside a data boundary with a
			// warning so the model treats it as data, not instructions.
			sb.WriteString(fmt.Sprintf("<data source=%q warning=\"document contains prompt-injection markers: %s\">\n%s\n</data>\n",
				doc.Name, strings.Join(doc.InjectionHits, ", "), doc.Content))
			continue
		}
		sb.WriteString(doc.Content + "\n")
	}
	sb.WriteString("## Auto Memory\n")
	if recent, merr := memory.Recent(workspaceRoot, autoMemoryInjectionLimit); merr == nil && len(recent) > 0 {
		for _, e := range recent {
			fields := e.Name + " " + e.Description + " " + e.Content
			if hits := memory.ScanAll(fields); len(hits) > 0 {
				sb.WriteString(fmt.Sprintf("<data source=\"auto-memory\" warning=\"injection markers: %s\">\n%s\n</data>\n",
					strings.Join(hits, ", "), truncateRunes(e.Content, 200)))
				continue
			}
			desc := e.Description
			if desc == "" {
				desc = e.Name
			}
			sb.WriteString(fmt.Sprintf("- **%s** — %s: %s\n", e.Name, desc, truncateRunes(e.Content, 100)))
		}
	} else {
		sb.WriteString("(none yet)\n")
	}
	sb.WriteString("\n## Available Skills\n")
	for _, sk := range skills {
		tag := ""
		if sk.IsSubagent {
			tag = " [subagent]"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s%s\n", sk.Name, sk.Description, tag))
	}
	sb.WriteString("\n")
	sb.WriteString(cmdStore.IndexBlock())
	return sb.String()
}

// autoMemoryInjectionLimit caps how many recent auto-memory entries are
// injected into the system prompt at startup (most recent first).
const autoMemoryInjectionLimit = 3 // P6 裁剪二轮：仅注入最相关记忆

// todoSummary implements agent.TodoSummaryProvider: it renders the current
// todo list (≤10 items) for injection into the system prompt tail.
type todoSummary struct {
	st        *store.Store
	sessionID string
}

func (t todoSummary) Summary() string {
	if t.st == nil || t.sessionID == "" {
		return ""
	}
	todos, err := t.st.LoadTodos(t.sessionID)
	if err != nil || len(todos) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Current Todos\n")
	for i, td := range todos {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("- … 还有 %d 项\n", len(todos)-10))
			break
		}
		marker := " "
		switch td.Status {
		case "completed":
			marker = "x"
		case "in_progress":
			marker = ">"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", marker, td.Content))
	}
	return sb.String()
}

// planContractText is appended to the system prompt in plan posture.
func planContractText() string {
	return `
## Plan Contract
你当前处于 plan 姿态：动手执行前必须先输出结构化计划，并用 todo_write 建立任务清单；计划中每一步对应一条 todo。所有非只读工具调用都需要用户批准，先计划再执行。每完成一步立即用 todo_write 更新对应状态（pending → in_progress → completed）。
`
}

// workspaceRootFor returns the configured workspace root, falling back to
// the process working directory when unset.
func workspaceRootFor(cfg *config.Config) string {
	if cfg.Sandbox.WorkspaceRoot != "" {
		return cfg.Sandbox.WorkspaceRoot
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// truncateRunes shortens s to at most n runes without splitting UTF-8.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// convertHooks translates config.HookConfig values into the hook package's
// HookConfig type. The two types are structurally identical; this helper
// exists to avoid a direct package dependency from config to hook.
func convertHooks(shellHooks []config.HookConfig) []hook.HookConfig {
	var result []hook.HookConfig
	for _, h := range shellHooks {
		result = append(result, hook.HookConfig{
			Event: h.Event, Matcher: h.Matcher, Command: h.Command, Timeout: h.Timeout,
		})
	}
	return result
}

// SwitchModel resolves "provider/model" (or a bare model when exactly one
// provider is configured) from the config, builds the provider through the
// same secrets pool as startup, and switches the controller to it. Returns the
// canonical display name on success.
func SwitchModel(ctrl *control.Controller, cfg *config.Config, spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("usage: /model provider/model（如 qwen/qwen3.8-max）")
	}
	provName, modelName := parseModel(spec)
	var provCfg *config.ProviderConfig
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == provName {
			provCfg = &cfg.Providers[i]
			break
		}
	}
	if provCfg == nil && len(cfg.Providers) == 1 {
		provCfg = &cfg.Providers[0]
	}
	if provCfg == nil {
		return "", fmt.Errorf("provider %q not found in config", provName)
	}
	var apiKey string
	if provCfg.Kind != "ollama" {
		keyPool, err := secrets.NewPool(provCfg.APIKeyEnv)
		if err != nil {
			return "", fmt.Errorf("api key for %s: %w", provName, err)
		}
		apiKey, err = keyPool.Get()
		if err != nil {
			return "", fmt.Errorf("api key for %s: %w", provName, err)
		}
	}
	prov, err := BuildProvider(provCfg.Kind, provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)
	if err != nil {
		return "", err
	}
	if err := ctrl.SwitchProvider(prov, modelName); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", provName, modelName), nil
}
