package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"bounty/internal/boot"
	"bounty/internal/channel"
	"bounty/internal/cli"
	"bounty/internal/config"
	"bounty/internal/event"
	"bounty/internal/permission"
	"bounty/internal/remote"
	"bounty/internal/repair"
	"bounty/internal/serve"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: bounty chat|run|serve|dashboard|doctor|remote\n")
		os.Exit(1)
	}
	switch os.Args[1] {
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

func chatCmd() {
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
		fmt.Printf("  %s  [%s] %s\n", s.ID[:20], t, s.Title)
	}
}

func runCmd() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: bounty run <prompt>\n")
		os.Exit(1)
	}
	prompt := os.Args[2]
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
		SessionID: fmt.Sprintf("oneshot-%d", time.Now().UnixNano()),
	})
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
}

func dashboardCmd() {
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
		SessionID: fmt.Sprintf("dashboard-%d", time.Now().UnixNano()),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	dashboard := &serve.DashboardHandler{
		SessionList: func() []serve.SessionInfo {
			sessions, _ := ctrl.ListSessions(20)
			var result []serve.SessionInfo
			for _, s := range sessions {
				result = append(result, serve.SessionInfo{
					ID:        s.ID,
					Title:     s.Title,
					Model:     s.Model,
					UpdatedAt: s.UpdatedAt,
				})
			}
			return result
		},
	}

	mux := http.NewServeMux()
	mux.Handle("/dashboard", dashboard)
	mux.Handle("/dashboard/", dashboard)
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Message string `json:"message"` }
		json.NewDecoder(r.Body).Decode(&req)
		ctrl.Send(r.Context(), req.Message)
		w.WriteHeader(202)
	})

	fmt.Println("Dashboard: http://localhost:8080/dashboard")
	http.ListenAndServe(":8080", mux)
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

// consoleSink prints events to the terminal.
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
