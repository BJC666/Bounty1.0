// desktop/main.go — Experimental Wails desktop app entry point.
// Build: cd desktop && wails build
package main

import (
	"context"
	"fmt"
	"os"

	"bounty/internal/boot"
	"bounty/internal/config"
	"bounty/internal/event"
	"bounty/internal/permission"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds the application context.
type App struct {
	ctx context.Context
}

// NewApp creates a new App instance.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// SendMessage processes a user message through the Bounty controller and emits
// events back to the Wails frontend.
func (a *App) SendMessage(text string) string {
	wd, _ := os.Getwd()
	cfg, _ := config.Load(wd)
	ctrl, _ := boot.Build(cfg, boot.Options{
		MaxSteps: 50,
		Sink:     &wailsSink{ctx: a.ctx},
		Posture:  permission.PostureAuto,
	})
	ctrl.Send(a.ctx, text)
	return "OK"
}

// wailsSink implements event.Sink by emitting Wails runtime events.
type wailsSink struct{ ctx context.Context }

func (s *wailsSink) Emit(ev event.Event) {
	switch ev.Type {
	case "text":
		runtime.EventsEmit(s.ctx, "agent-text", ev.TextDelta)
	case "tool_call":
		runtime.EventsEmit(s.ctx, "agent-tool", ev.ToolName)
	case "turn_complete":
		runtime.EventsEmit(s.ctx, "agent-done", "")
	}
}
