package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bounty/internal/auth"
	"bounty/internal/boot"
	"bounty/internal/channel"
	"bounty/internal/checkpoint"
	"bounty/internal/cli"
	"bounty/internal/config"
	"bounty/internal/event"
	"bounty/internal/permission"
	"bounty/internal/remote"
	"bounty/internal/repair"
	"bounty/internal/serve"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: bounty chat|run|serve|dashboard|doctor|remote\n")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "help", "--help", "-h":
		fmt.Print(rootUsage)
		return
	case "chat":
		chatCmd()
	case "run":
		runCmd()
	case "serve":
		serveCmd()
	case "doctor":
		doctorCmd()
	case "dashboard":
		dashboardCmd()
	case "remote":
		remoteCmd()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

const chatUsage = `Bounty Chat TUI（终端交互界面）

用法:
  bounty chat                新建会话进入 TUI
  bounty chat --resume <id>  恢复指定会话
  bounty chat --list         列出最近会话后退出

TUI 按键:
  Enter        发送；Shift+Enter 换行
  Alt+↑/Alt+↓  上一条 / 下一条输入历史
  ↑/↓ 滚动输出，PgUp/PgDown/Home/End 快速滚动
  e            展开最近一次工具调用的参数与结果
  E            展开 / 折叠全部工具调用
  Esc          清除当前输入（工具弹窗中为拒绝）
  数字键        在权限确认弹窗中选择方案

斜杠命令:
  /status              会话状态（模型 / token / 轮次 / 工具数）
  /model [provider/model]  查看或切换模型（如 openai/deepseek-chat）
  /compact             立即压缩上下文
  /todo                查看待办
  /export [文件.md]    导出会话为 Markdown（默认会话名.md）
  /skills              列出可用技能
  /new /switch /list /rename /help  会话管理
`

func chatCmd() {
	for _, arg := range os.Args {
		if arg == "--help" || arg == "-h" {
			fmt.Print(chatUsage)
			return
		}
	}

	wd, _ := os.Getwd()
	cfg, err := repair.SafeLoad(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Check for --list first
	for _, arg := range os.Args {
		if arg == "--list" {
			listSessions(cfg)
			return
		}
	}

	// Check for --resume flag
	var resumeID string
	for i, arg := range os.Args {
		if arg == "--resume" && i+1 < len(os.Args) {
			resumeID = os.Args[i+1]
		}
	}

	var sessionID string
	if resumeID != "" {
		sessionID = resumeID
	} else {
		sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	// Launch TUI (boot.Build is called inside RunTUI with the TUI sink)
	if err := cli.RunTUI(cfg, sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

func listSessions(cfg *config.Config) {
	// Quick bootstrap just for listing
	ctrl, err := boot.Build(cfg, boot.Options{
		Sink:      &consoleSink{},
		Posture:   permission.PostureAuto,
		SessionID: "list-only",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	sessions, err := ctrl.ListSessions(20)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Recent sessions:")
	for _, s := range sessions {
		t := time.Unix(s.UpdatedAt, 0).Format("2006-01-02 15:04")
		id := s.ID
		if len(id) > 20 {
			id = id[:20]
		}
		fmt.Printf("  %s  [%s] %s\n", id, t, s.Title)
	}
}

func runCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: bounty run <prompt> [--json] [--model provider/model] [--max-steps N]\n")
		os.Exit(1)
	}
	prompt := os.Args[2]

	// Optional flags (kept simple: --flag or --flag=value, after the prompt).
	var (
		useJSON  bool
		model    string
		maxSteps int
	)
	for _, arg := range os.Args[3:] {
		switch {
		case arg == "--json":
			useJSON = true
		case strings.HasPrefix(arg, "--model="):
			model = strings.TrimPrefix(arg, "--model=")
		case strings.HasPrefix(arg, "--max-steps="):
			if n, err := strconv.Atoi(strings.TrimPrefix(arg, "--max-steps=")); err == nil && n > 0 {
				maxSteps = n
			}
		}
	}

	wd, _ := os.Getwd()
	cfg, err := repair.SafeLoad(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	opts := boot.Options{
		MaxSteps:  cfg.Agent.MaxSteps,
		Sink:      event.Sink(&consoleSink{}),
		Posture:   permission.PostureAuto,
		SessionID: fmt.Sprintf("oneshot-%d", time.Now().UnixNano()),
		Asker:     cli.TerminalAsker{},
	}
	if model != "" {
		opts.Model = model
	}
	if maxSteps > 0 {
		opts.MaxSteps = maxSteps
	}
	if useJSON {
		// Structured output for eval/automation: one JSON event per line,
		// and fail closed on Ask decisions instead of reading stdin.
		opts.Sink = newJSONSink(os.Stdout)
		opts.Asker = nil
	}

	ctrl, err := boot.Build(cfg, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := ctrl.Send(ctx, prompt); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func serveCmd() {
	wd, _ := os.Getwd()
	cfg, err := repair.SafeLoad(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctrl, err := boot.Build(cfg, boot.Options{
		MaxSteps:  cfg.Agent.MaxSteps,
		Sink:      &consoleSink{},
		Posture:   permission.PostureAuto,
		SessionID: fmt.Sprintf("serve-%d", time.Now().UnixNano()),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	chanReg := boot.NewChannelRegistry(ctrl)
	ctx := context.Background()
	gw := channel.NewGateway(ctrl, chanReg, 8080)
	if err := gw.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Gateway error: %v\n", err)
	}
}

func doctorCmd() {
	// Check for --repair flag first
	for _, arg := range os.Args {
		if arg == "--repair" {
			fmt.Println("Attempting config repair from snapshot...")
			cfg, err := repair.RestoreSnapshot()
			if err != nil {
				fmt.Printf("❌ Repair failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✅ Restored config: model=%s, providers=%d\n", cfg.DefaultModel, len(cfg.Providers))
			return
		}
	}

	fmt.Println("Bounty Doctor — checking configuration...")
	wd, _ := os.Getwd()
	cfg, err := config.Load(wd)
	if err != nil {
		fmt.Printf("❌ Config load failed: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Printf("❌ Config invalid: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Config valid\n")
	fmt.Printf("   Default model: %s\n", cfg.DefaultModel)
	fmt.Printf("   Providers: %d\n", len(cfg.Providers))
	for _, p := range cfg.Providers {
		keyStatus := "✅"
		if os.Getenv(p.APIKeyEnv) == "" {
			keyStatus = "⚠️  not set"
		}
		fmt.Printf("   - %s (%s): %s %s\n", p.Name, p.Kind, p.APIKeyEnv, keyStatus)
	}
	fmt.Printf("   Max steps: %d\n", cfg.Agent.MaxSteps)
	fmt.Printf("   Compact ratio: %.1f\n", cfg.Agent.CompactRatio)
	fmt.Printf("   Tools: 20 total (12 builtin + 5 DeVET + 3 subagent)\n")
}

func dashboardCmd() {
	wd, _ := os.Getwd()
	cfg, err := repair.SafeLoad(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Broadcast sink for SSE clients
	broadcast := newBroadcastSink()

	ctrl, err := boot.Build(cfg, boot.Options{
		MaxSteps:  cfg.Agent.MaxSteps,
		Sink:      broadcast,
		Posture:   permission.PostureAuto,
		SessionID: fmt.Sprintf("web-%d", time.Now().UnixNano()),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	restorer := ctrl.CheckpointRestorer()
	chatHandler := &serve.ChatHandler{
		SendFn: func(text string) error { return ctrl.Send(context.Background(), text) },
		SwitchFn: func(req serve.ModelSwitchRequest) error {
			kind := req.Kind
			if kind == "" {
				kind = "openai"
			}
			prov, err := boot.BuildProvider(kind, req.BaseURL, req.APIKey, req.Model, 0)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := boot.TestProvider(ctx, prov); err != nil {
				return fmt.Errorf("连接测试失败: %v", err)
			}
			return ctrl.SwitchProvider(prov, req.Model)
		},
		CheckpointListFn: func() ([]checkpoint.Info, error) {
			if restorer == nil {
				return nil, fmt.Errorf("检查点不可用（git 未安装或无会话）")
			}
			return restorer.ListCheckpoints()
		},
		CheckpointRestoreFn: func(msgIndex int) error {
			if restorer == nil {
				return fmt.Errorf("检查点不可用（git 未安装或无会话）")
			}
			return restorer.RestoreCheckpoint(msgIndex)
		},
	}

	dashboard := &serve.DashboardHandler{
		SessionList: func() []serve.SessionInfo {
			sessions, _ := ctrl.ListSessions(20)
			var result []serve.SessionInfo
			for _, s := range sessions {
				result = append(result, serve.SessionInfo{ID: s.ID, Title: s.Title, Model: s.Model, UpdatedAt: s.UpdatedAt})
			}
			return result
		},
	}

	exportHandler := &serve.ExportHandler{
		LoadSessionFn:  ctrl.GetStore().LoadSession,
		LoadMessagesFn: ctrl.GetStore().LoadMessages,
	}

	mux := http.NewServeMux()
	mux.Handle("/", auth.Middleware(chatHandler))
	mux.Handle("/dashboard", auth.Middleware(dashboard))
	mux.Handle("/dashboard/", auth.Middleware(dashboard))
	mux.Handle("/export", auth.Middleware(exportHandler))
	mux.Handle("/events", auth.Middleware(http.HandlerFunc(broadcast.serveSSE)))

	fmt.Println("Dashboard: http://localhost:8090/dashboard")
	server := &http.Server{
		Addr:              ":8090",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func remoteCmd() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: bounty remote <host> <command>\n")
		os.Exit(1)
	}
	sess := remote.NewSSH(os.Args[2], os.Getenv("USER"), "", 22)
	output, err := sess.Run(context.Background(), strings.Join(os.Args[3:], " "))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(output)
}

const rootUsage = `Bounty — 智能体 CLI

用法:
  bounty chat            终端 TUI 对话（bounty chat --help 查看按键与斜杠命令）
  bounty run <prompt>    单次任务（--json --model provider/model --max-steps N）
  bounty serve           多渠道网关（端口 8080）
  bounty dashboard       网页版控制台（http://localhost:8090/dashboard）
  bounty doctor          配置体检（--repair 从快照恢复）
  bounty remote <host> <command>  通过 SSH 远程执行
`

// consoleSink prints events to the terminal.
// broadcastSink fans out events to all connected SSE clients.
type broadcastSink struct {
	clients map[chan string]bool
	mu      sync.Mutex
}

func newBroadcastSink() *broadcastSink { return &broadcastSink{clients: make(map[chan string]bool)} }
func (b *broadcastSink) Emit(ev event.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SSE: json marshal error: %v\n", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.clients) == 0 {
		fmt.Fprintf(os.Stderr, "SSE: no clients connected, dropping event type=%s\n", ev.Type)
		return
	}
	for ch := range b.clients {
		select {
		case ch <- string(data):
		default:
			fmt.Fprintf(os.Stderr, "SSE: dropping event type=%s (buffer full)\n", ev.Type)
		}
	}
}

func (b *broadcastSink) serveSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}
	ch := make(chan string, 512)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.clients, ch); close(ch); b.mu.Unlock() }()
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			// Extend the write deadline so an idle SSE stream survives the
			// server-level WriteTimeout.
			http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

type consoleSink struct{}

func (s *consoleSink) Emit(ev event.Event) {
	switch ev.Type {
	case "reasoning":
		// silent — reasoning is internal
	case "text":
		fmt.Print(ev.TextDelta)
	case "tool_call":
		fmt.Printf("\n🔧 %s...", ev.ToolName)
	case "tool_result":
		if ev.ToolErr != "" {
			fmt.Printf(" ❌ %s", ev.ToolErr)
		} else {
			fmt.Print(" ✅")
		}
	case "usage":
		fmt.Printf("\n[%d→%d tokens]\n", ev.Usage.InputTokens, ev.Usage.OutputTokens)
	case "turn_complete":
		fmt.Println()
	}
}
