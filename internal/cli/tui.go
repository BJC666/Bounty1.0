package cli

import (
	"context"
	"fmt"
	"strings"

	"bounty/internal/boot"
	"bounty/internal/config"
	"bounty/internal/control"
	"bounty/internal/event"
	"bounty/internal/permission"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Padding(0, 1)
	toolStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	infoStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	userStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
)

type model struct {
	ctrl       *control.Controller
	ctx        context.Context
	input      strings.Builder
	output     strings.Builder
	thinking   strings.Builder
	toolStatus string
	errMsg     string
	quitting   bool
	ready      bool
}

func newModel(ctrl *control.Controller) *model {
	return &model{ctrl: ctrl, ctx: context.Background(), ready: true}
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			if m.input.Len() == 0 {
				return m, nil
			}
			text := strings.TrimSpace(m.input.String())
			m.input.Reset()
			m.errMsg = ""
			m.output.Reset()
			m.thinking.Reset()
			m.toolStatus = ""
			return m, m.sendMessage(text)
		case tea.KeyBackspace:
			s := m.input.String()
			if len(s) > 0 {
				m.input.Reset()
				m.input.WriteString(s[:len(s)-1])
			}
			return m, nil
		default:
			if len(msg.Runes) == 1 {
				m.input.WriteRune(msg.Runes[0])
			}
			return m, nil
		}
	case agentResponse:
		m.output.WriteString(msg.text)
		m.toolStatus = ""
		return m, nil
	case agentThinking:
		m.thinking.WriteString(msg.text)
		return m, nil
	case agentToolCall:
		m.toolStatus = fmt.Sprintf("🔧 %s...", msg.name)
		return m, nil
	case agentToolResult:
		if msg.err != "" {
			m.toolStatus = fmt.Sprintf("❌ %s: %s", msg.name, msg.err)
		} else {
			m.toolStatus = "✅ done"
		}
		return m, nil
	case agentError:
		m.errMsg = msg.err.Error()
		return m, nil
	case tea.WindowSizeMsg:
		return m, nil
	}
	return m, nil
}

func (m *model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(" Bounty Agent "))
	sb.WriteString(infoStyle.Render("  Ctrl+C to quit"))
	sb.WriteString("\n\n")

	if m.thinking.Len() > 0 {
		sb.WriteString(thinkingStyle.Render(m.thinking.String()))
	}
	if m.output.Len() > 0 {
		sb.WriteString(m.output.String())
		sb.WriteString("\n")
	}
	if m.toolStatus != "" {
		sb.WriteString(toolStyle.Render(m.toolStatus))
		sb.WriteString("\n")
	}
	if m.errMsg != "" {
		sb.WriteString(errStyle.Render("Error: " + m.errMsg))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(userStyle.Render("> "))
	sb.WriteString(m.input.String())
	sb.WriteString("█")
	sb.WriteString("\n")

	return sb.String()
}

func (m *model) sendMessage(text string) tea.Cmd {
	return func() tea.Msg {
		err := m.ctrl.Send(m.ctx, text)
		if err != nil {
			return agentError{err: err}
		}
		return agentResponse{text: ""}
	}
}

// Message types
type agentResponse struct{ text string }
type agentThinking struct{ text string }
type agentToolCall struct{ name string }
type agentToolResult struct{ name, err string }
type agentError struct{ err error }

// tuiSink implements event.Sink, bridging agent events to TUI messages.
type tuiSink struct {
	program *tea.Program
}

func (s *tuiSink) Emit(ev event.Event) {
	if s.program == nil {
		return
	}
	switch ev.Type {
	case "text":
		s.program.Send(agentResponse{text: ev.TextDelta})
	case "reasoning":
		s.program.Send(agentThinking{text: ev.ReasoningDelta})
	case "tool_call":
		s.program.Send(agentToolCall{name: ev.ToolName})
	case "tool_result":
		s.program.Send(agentToolResult{name: ev.ToolName, err: ev.ToolErr})
	}
}

// RunTUI starts the Bubbletea TUI. It builds the controller internally so the
// TUI sink is properly wired to the agent from construction time.
func RunTUI(cfg *config.Config, sessionID string) error {
	m := newModel(nil) // controller wired after build
	p := tea.NewProgram(m)

	sink := &tuiSink{program: p}

	ctrl, err := boot.Build(cfg, boot.Options{
		MaxSteps:  cfg.Agent.MaxSteps,
		Sink:      sink,
		Posture:   permission.PostureAuto,
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}

	m.ctrl = ctrl

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
