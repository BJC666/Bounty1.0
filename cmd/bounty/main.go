package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bounty/internal/boot"
	"bounty/internal/config"
	"bounty/internal/event"
	"bounty/internal/permission"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: bounty chat|run|doctor\n")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "chat":
		chatCmd()
	case "run":
		runCmd()
	case "doctor":
		doctorCmd()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func chatCmd() {
	wd, _ := os.Getwd()
	cfg, err := config.Load(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Check for --list flag
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
		fmt.Printf("Resuming session %s...\n", resumeID)
	} else {
		sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	ctrl, err := boot.Build(cfg, boot.Options{
		MaxSteps:  cfg.Agent.MaxSteps,
		Sink:      &consoleSink{},
		Posture:   permission.PostureAuto,
		SessionID: sessionID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building agent: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	fmt.Println("Bounty Agent — type your message (/exit to quit, /save to persist)")
	for {
		fmt.Print("> ")
		var input string
		if _, err := fmt.Scanln(&input); err != nil {
			break
		}
		if input == "/exit" {
			break
		}
		if input == "/save" {
			if err := ctrl.SaveTurn(); err != nil {
				fmt.Fprintf(os.Stderr, "Save error: %v\n", err)
			} else {
				fmt.Println("Session saved.")
			}
			continue
		}
		if input == "" {
			continue
		}

		if err := ctrl.Send(ctx, input); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		// Auto-save after each turn
		ctrl.SaveTurn()
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
	cfg, err := config.Load(wd)
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

func doctorCmd() {
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
