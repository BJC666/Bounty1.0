package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bounty/internal/agent"
	"bounty/internal/config"
	"bounty/internal/control"
	"bounty/internal/event"
	"bounty/internal/hook"
	"bounty/internal/memory"
	"bounty/internal/permission"
	"bounty/internal/provider"
	"bounty/internal/provider/anthropic"
	"bounty/internal/provider/openai"
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

	// 3. Load API key
	apiKey, err := secrets.LoadFromEnv(provCfg.APIKeyEnv)
	if err != nil {
		return nil, fmt.Errorf("api key for %s: %w", provName, err)
	}

	// 4. Create Provider
	var prov provider.Provider
	switch provCfg.Kind {
	case "openai":
		prov = openai.New(provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)
	case "anthropic":
		prov = anthropic.New(provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)
	default:
		return nil, fmt.Errorf("unknown provider kind: %s", provCfg.Kind)
	}

	// 5. Create tool registry + register builtins
	reg := tool.NewRegistry()
	builtin.RegisterAll(reg, builtin.ToolOptions{BashTimeout: 120e9})

	// 6. Load memory
	memDocs, _ := memory.Load(cfg.Sandbox.WorkspaceRoot)

	// 7. Load skills
	skillStore := skill.NewStore()
	skillStore.Discover(cfg.Skills.Paths)

	// 8. Build system prompt
	systemPrompt := buildSystemPrompt(cfg, memDocs, skillStore.Index())

	// 9. Create Session
	session := agent.NewSession(systemPrompt)

	// 10. Create permission gate
	permGate := permission.NewGate(cfg.Permissions, opts.Posture)

	// 11. Create hook runner
	var hookRunner *hook.Runner
	if cfg.Hooks.Enabled {
		hookRunner = hook.NewRunner(convertHooks(cfg.Hooks.Shell))
	}

	// 12. Create Agent
	ag := agent.New(prov, reg, session, agent.Options{
		MaxSteps:    opts.MaxSteps,
		Temperature: cfg.Agent.Temperature,
		Sink:        opts.Sink,
		Gate:        gateAdapter{gate: permGate},
		Hooks:       hookAdapter{runner: hookRunner},
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
	ctrl := control.New(ag, opts.Sink, st, hookRunner, permGate, skillStore, opts.SessionID)
	return ctrl, nil
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

func (a hookAdapter) PostToolUse(ctx context.Context, name string, result string, execErr error) {
	// fire-and-forget for PostToolUse
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
func dataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "bounty")
}

// buildSystemPrompt constructs the system prompt by combining the base
// persona, workspace root, project memory documents, and available skills.
func buildSystemPrompt(cfg *config.Config, docs []memory.Doc, skills []skill.IndexEntry) string {
	var sb strings.Builder
	sb.WriteString("You are Bounty, a general-purpose AI agent. You help users with software engineering, research, data analysis, and automation tasks.\n\n")
	if cfg.Sandbox.WorkspaceRoot != "" {
		sb.WriteString("## Workspace\n" + cfg.Sandbox.WorkspaceRoot + "\n\n")
	}
	sb.WriteString("## Project Memory\n")
	for _, doc := range docs {
		sb.WriteString(doc.Content + "\n")
	}
	sb.WriteString("\n## Available Skills\n")
	for _, sk := range skills {
		tag := ""
		if sk.IsSubagent {
			tag = " [subagent]"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s%s\n", sk.Name, sk.Description, tag))
	}
	return sb.String()
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
