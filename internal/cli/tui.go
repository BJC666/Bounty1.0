package cli

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"bounty/internal/boot"
	"bounty/internal/config"
	"bounty/internal/control"
	"bounty/internal/event"
	"bounty/internal/permission"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	goldC   = lipgloss.Color("#FFD700")
	creamC  = lipgloss.Color("#FFF8DC")
	navyC   = lipgloss.Color("#1a1a2e")
	silverC = lipgloss.Color("#C0C0C0")
	mutedC  = lipgloss.Color("#8B8682")
	dimgrayC = lipgloss.Color("#969696")
	purpleC = lipgloss.Color("#9B8EC4")
	redC    = lipgloss.Color("#FF6B6B")
	sBg     = lipgloss.Color("#2a2a3e")
	sFg     = lipgloss.Color("#555570")

	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	thinkingFaces = []string{"(｡•́︿•̀｡)", "(◔_◔)", "(¬‿¬)", "◉_◉", "(⊙_⊙)", "ಠ_ಠ"}
	thinkingVerbs = []string{"pondering", "contemplating", "musing", "cogitating", "ruminating", "deliberating", "mulling", "reflecting", "processing", "reasoning"}

	toolEmoji = map[string]string{
		"read_file": "📖", "write_file": "✍️", "edit_file": "🔧", "multi_edit": "🔧",
		"grep": "🔎", "glob": "🔎", "web_search": "🔍", "web_fetch": "📄",
		"bash": "💻", "browser": "🌐", "code_index": "🔍",
		"todo_write": "📋", "remember": "🧠",
		"task": "🔀", "read_only_task": "🔍", "fleet": "🔀",
		"devet_health": "🏥", "devet_build_scenario": "🏗️",
		"devet_verify_chain": "✅", "devet_list_attacks": "🛡️", "devet_simulate_attack": "⚠️",
	}

	accentBold = lipgloss.NewStyle().Foreground(goldC).Bold(true)
	accentText = lipgloss.NewStyle().Foreground(goldC)
	creamBold  = lipgloss.NewStyle().Foreground(creamC).Bold(true)
	creamText  = lipgloss.NewStyle().Foreground(creamC)
	mutedText  = lipgloss.NewStyle().Foreground(mutedC)
	dimText    = lipgloss.NewStyle().Foreground(dimgrayC).Italic(true)
	purpleText = lipgloss.NewStyle().Foreground(purpleC).Italic(true)
	errText    = lipgloss.NewStyle().Foreground(redC).Bold(true)
	navyGold   = lipgloss.NewStyle().Background(navyC).Foreground(goldC).Bold(true)
	navySilver = lipgloss.NewStyle().Background(navyC).Foreground(silverC)
	navyMuted  = lipgloss.NewStyle().Background(navyC).Foreground(mutedC)
	scrollTrack = lipgloss.NewStyle().Background(sBg)
	scrollThumb = lipgloss.NewStyle().Background(sFg)
)

type histEntry struct{ kind, text string; time time.Time }

type tuiModel struct {
	ctrl      *control.Controller
	cfg       *config.Config
	modelName, sessionID string
	inputBuf  strings.Builder
	history   []histEntry
	thinking  strings.Builder
	width, height int
	quitting  bool
	errMsg    string
	startTime   time.Time
	totalTokens, ioTokens, ooTokens, toolCalls, turnCount int
	totalCost  float64
	spinnerIdx int
	isThinking bool
	currentTool string
	scrollPos, maxScroll int
	dragging   bool
	dragStartY, dragStartPos int
}

func newTUIModel(cfg *config.Config, sid string) *tuiModel {
	return &tuiModel{cfg: cfg, modelName: cfg.DefaultModel, sessionID: sid, startTime: time.Now(), history: make([]histEntry, 0)}
}

func (m *tuiModel) Init() tea.Cmd { return tea.Batch(tickCmd(), tea.EnterAltScreen) }
func tickCmd() tea.Cmd { return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }) }
type tickMsg time.Time

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		return m, tickCmd()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	case agentTextMsg:
		m.isThinking = false; m.thinking.Reset(); m.scrollPos = 0
		n := len(m.history)
		if n > 0 && m.history[n-1].kind == "assist" {
			m.history[n-1].text += msg.text
		} else {
			m.history = append(m.history, histEntry{kind: "assist", text: msg.text})
		}
		return m, nil
	case agentReasoningMsg:
		m.thinking.WriteString(msg.text); return m, nil
	case agentToolMsg:
		m.toolCalls++; m.currentTool = msg.name
		face := thinkingFaces[rand.Intn(len(thinkingFaces))]
		verb := thinkingVerbs[rand.Intn(len(thinkingVerbs))]
		m.history = append(m.history, histEntry{kind: "thinking", text: fmt.Sprintf("%s  %s %s...", face, verb, emojiFor(msg.name))})
		return m, nil
	case agentToolDoneMsg:
		return m, nil
	case agentToolErrMsg:
		m.history = append(m.history, histEntry{kind: "error", text: fmt.Sprintf("   ❌ %s", msg.err)})
		return m, nil
	case agentErrMsg:
		m.errMsg = msg.err.Error(); m.isThinking = false; return m, nil
	case agentTokensMsg:
		m.totalTokens += msg.total; m.totalCost += msg.cost
		m.ioTokens += msg.input; m.ooTokens += msg.output; return m, nil
	}
	return m, nil
}

func (m *tuiModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	w, h := m.width, m.height
	if w < 60 { w = 80 }
	if h < 10 { h = 30 }
	msgH := h - 5
	scrollW := 3
	scrollX := w - scrollW - 2

	if m.maxScroll > 0 {
		thumbH := msgH * msgH / (len(m.history) + msgH)
		if thumbH < 2 { thumbH = 2 }
		thumbTop := 0
		if m.maxScroll > 0 {
			thumbTop = (msgH - thumbH) * (m.maxScroll - m.scrollPos) / m.maxScroll
		}
		if thumbTop < 0 { thumbTop = 0 }
		if thumbTop+thumbH > msgH { thumbTop = msgH - thumbH }

		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollPos++; if m.scrollPos > m.maxScroll { m.scrollPos = m.maxScroll }
		case tea.MouseButtonWheelDown:
			m.scrollPos--; if m.scrollPos < 0 { m.scrollPos = 0 }
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				if msg.X >= scrollX && msg.X <= scrollX+scrollW {
					if msg.Y >= thumbTop && msg.Y <= thumbTop+thumbH {
						m.dragging = true; m.dragStartY = msg.Y; m.dragStartPos = m.scrollPos
					} else if msg.Y < thumbTop {
						m.scrollPos += 5; if m.scrollPos > m.maxScroll { m.scrollPos = m.maxScroll }
					} else {
						m.scrollPos -= 5; if m.scrollPos < 0 { m.scrollPos = 0 }
					}
				}
			}
			if msg.Action == tea.MouseActionRelease { m.dragging = false }
		case tea.MouseButtonNone:
			if m.dragging {
				dy := msg.Y - m.dragStartY
				avail := msgH - thumbH
				if avail > 0 {
					step := float64(m.maxScroll) / float64(avail)
					m.scrollPos = m.dragStartPos - int(float64(dy)*step)
					if m.scrollPos < 0 { m.scrollPos = 0 }
					if m.scrollPos > m.maxScroll { m.scrollPos = m.maxScroll }
				}
			}
		}
	}
	return m, nil
}

func (m *tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		m.quitting = true; return m, tea.Quit
	case tea.KeyPgUp:
		m.scrollPos += 10; if m.scrollPos > m.maxScroll { m.scrollPos = m.maxScroll }; return m, nil
	case tea.KeyPgDown:
		m.scrollPos -= 10; if m.scrollPos < 0 { m.scrollPos = 0 }; return m, nil
	case tea.KeyHome:
		m.scrollPos = m.maxScroll; return m, nil
	case tea.KeyEnd:
		m.scrollPos = 0; return m, nil
	case tea.KeyUp:
		m.scrollPos++; if m.scrollPos > m.maxScroll { m.scrollPos = m.maxScroll }; return m, nil
	case tea.KeyDown:
		m.scrollPos--; if m.scrollPos < 0 { m.scrollPos = 0 }; return m, nil
	case tea.KeyEnter:
		text := strings.TrimSpace(m.inputBuf.String())
		if text == "" { return m, nil }
		m.inputBuf.Reset(); m.errMsg = ""; m.thinking.Reset()
		m.isThinking = true; m.currentTool = ""; m.turnCount++; m.scrollPos = 0
		m.history = append(m.history, histEntry{kind: "user", text: text})
		return m, m.sendMessage(text)
	case tea.KeyBackspace:
		s := m.inputBuf.String()
		if len(s) > 0 { m.inputBuf.Reset(); m.inputBuf.WriteString(s[:len(s)-1]) }
		return m, nil
	case tea.KeySpace:
		m.inputBuf.WriteRune(' '); return m, nil
	default:
		if len(msg.Runes) == 1 { m.inputBuf.WriteRune(msg.Runes[0]) }
		return m, nil
	}
}

func (m *tuiModel) View() string {
	if m.quitting {
		return accentBold.Render(fmt.Sprintf("\n  Goodbye · %d turns · %d tokens · %s\n\n", m.turnCount, m.totalTokens, time.Since(m.startTime).Round(time.Second)))
	}
	w, h := m.width, m.height
	if w < 60 { w = 80 }; if h < 10 { h = 30 }
	scrollW := 3
	contentW := w - scrollW - 2
	msgH := h - 4
	if msgH < 3 { msgH = 3 }

	var sb strings.Builder

	// Header
	sb.WriteString(accentText.Render("╭─⚕ Bounty" + strings.Repeat("─", contentW-10) + "╮\n"))

	// Messages + scrollbar
	totalMsgs := len(m.history)
	m.maxScroll = totalMsgs - msgH
	if m.maxScroll < 0 { m.maxScroll = 0 }
	if m.scrollPos > m.maxScroll { m.scrollPos = m.maxScroll }
	if m.scrollPos < 0 { m.scrollPos = 0 }

	start := totalMsgs - msgH - m.scrollPos
	if start < 0 { start = 0 }
	end := start + msgH
	if end > totalMsgs { end = totalMsgs }
	visible := m.history
	if start < end { visible = visible[start:end] }

	thumbH, thumbTop := 0, 0
	if m.maxScroll > 0 {
		thumbH = msgH * msgH / (totalMsgs + msgH)
		if thumbH < 2 { thumbH = 2 }
		thumbTop = (msgH - thumbH) * (m.maxScroll - m.scrollPos) / m.maxScroll
		if thumbTop < 0 { thumbTop = 0 }
		if thumbTop+thumbH > msgH { thumbTop = msgH - thumbH }
	}

	idx := 0
	for i := 0; i < msgH; i++ {
		line := ""
		if i < msgH-len(visible) {
			line = ""
		} else if idx < len(visible) {
			line = m.renderLine(visible[idx], contentW)
			idx++
		}
		pad := contentW - lipgloss.Width(line)
		if pad < 0 { pad = 0 }
		line += strings.Repeat(" ", pad)

		sc := " "
		if m.maxScroll > 0 {
			if i >= thumbTop && i < thumbTop+thumbH {
				sc = scrollThumb.Render("█")
			} else {
				sc = scrollTrack.Render("│")
			}
		}
		sb.WriteString(line + " " + sc + "\n")
	}

	// Thinking
	if m.isThinking {
		spin := spinnerFrames[m.spinnerIdx%len(spinnerFrames)]
		face := thinkingFaces[rand.Intn(len(thinkingFaces))]
		verb := thinkingVerbs[rand.Intn(len(thinkingVerbs))]
		ti := spin + " " + face + " " + verb + "..."
		if m.currentTool != "" { ti += "  " + emojiFor(m.currentTool) + " " + m.currentTool }
		if m.thinking.Len() > 0 {
			t := m.thinking.String(); if len(t) > 60 { t = t[len(t)-60:] }; ti += "  " + t
		}
		sb.WriteString(dimText.Render("  " + ti) + "\n")
	}

	// Footer
	sb.WriteString(accentText.Render("╰" + strings.Repeat("─", contentW) + "╯\n"))

	// Input preview (shown above in message style)
	if m.inputBuf.Len() > 0 {
		sb.WriteString(accentBold.Render("● ") + creamBold.Render(m.inputBuf.String()) + "\n")
	}
	// Input line
	sb.WriteString(accentBold.Render("▸ "))
	if m.inputBuf.Len() > 0 {
		sb.WriteString(creamText.Render(m.inputBuf.String()))
	}
	if m.isThinking { sb.WriteString(spinnerFrames[m.spinnerIdx%len(spinnerFrames)]) }
	sb.WriteString("\n")

	// Status bar
	elapsed := time.Since(m.startTime).Round(time.Second)
	left := navyGold.Render(" ⚕ Bounty ") + navySilver.Render(" "+m.modelName+" ")
	left += navyMuted.Render(fmt.Sprintf("│ %d tok │ turn %d ", m.ioTokens+m.ooTokens, m.turnCount))
	right := navyMuted.Render(" " + elapsed.String() + " ")
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	pad := w - lw - rw; if pad < 1 { pad = 1 }
	sb.WriteString(lipgloss.NewStyle().Background(navyC).Width(w).Render(left + strings.Repeat(" ", pad) + right))
	return sb.String()
}

func (m *tuiModel) renderLine(h histEntry, w int) string {
	switch h.kind {
	case "user":  return accentBold.Render("● ") + creamBold.Render(trunc(h.text, w-4))
	case "assist": return "    " + creamText.Render(trunc(h.text, w-6))
	case "thinking": return dimText.Render("┊ " + trunc(h.text, w-4))
	case "error": return errText.Render("  " + trunc(h.text, w-4))
	}
	return trunc(h.text, w)
}

func (m *tuiModel) sendMessage(text string) tea.Cmd {
	return func() tea.Msg {
		if err := m.ctrl.Send(context.Background(), text); err != nil { return agentErrMsg{err: err} }
		return agentTextMsg{text: ""}
	}
}

func emojiFor(n string) string { if e, ok := toolEmoji[n]; ok { return e }; return "⚡" }
func trunc(s string, max int) string { if len(s) <= max { return s }; return s[:max-1] + "…" }

type agentTextMsg struct{ text string }
type agentReasoningMsg struct{ text string }
type agentToolMsg struct{ name string }
type agentToolDoneMsg struct{}
type agentToolErrMsg struct{ name, err string }
type agentErrMsg struct{ err error }
type agentTokensMsg struct{ total, input, output int; cost float64 }

type tuiSink struct{ program *tea.Program }
func (s *tuiSink) Emit(ev event.Event) {
	if s.program == nil { return }
	switch ev.Type {
	case "text":      s.program.Send(agentTextMsg{text: ev.TextDelta})
	case "reasoning": s.program.Send(agentReasoningMsg{text: ev.ReasoningDelta})
	case "tool_call":  s.program.Send(agentToolMsg{name: ev.ToolName})
	case "tool_result":
		if ev.ToolErr != "" { s.program.Send(agentToolErrMsg{name: ev.ToolName, err: ev.ToolErr}) } else { s.program.Send(agentToolDoneMsg{}) }
	case "usage":
		t, i, o := 0, 0, 0
		if ev.Usage != nil { i = ev.Usage.InputTokens; o = ev.Usage.OutputTokens; t = i + o }
		c := float64(i)*0.28/1_000_000 + float64(o)*1.10/1_000_000
		s.program.Send(agentTokensMsg{total: t, input: i, output: o, cost: c})
	}
}

func RunTUI(cfg *config.Config, sid string) error {
	m := newTUIModel(cfg, sid)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	s := &tuiSink{program: p}
	c, err := boot.Build(cfg, boot.Options{MaxSteps: cfg.Agent.MaxSteps, Sink: s, Posture: permission.PostureAuto, SessionID: sid})
	if err != nil { return err }
	m.ctrl = c
	_, err = p.Run()
	return err
}
