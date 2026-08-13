package cli

import (
	"context"
	"encoding/json"
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
	goldC    = lipgloss.Color("#FFD700")
	creamC   = lipgloss.Color("#FFF8DC")
	navyC    = lipgloss.Color("#1a1a2e")
	silverC  = lipgloss.Color("#C0C0C0")
	mutedC   = lipgloss.Color("#8B8682")
	dimgrayC = lipgloss.Color("#969696")
	purpleC  = lipgloss.Color("#9B8EC4")
	redC     = lipgloss.Color("#FF6B6B")
	sBg      = lipgloss.Color("#2a2a3e")
	sFg      = lipgloss.Color("#555570")

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

	accentBold  = lipgloss.NewStyle().Foreground(goldC).Bold(true)
	accentText  = lipgloss.NewStyle().Foreground(goldC)
	creamBold   = lipgloss.NewStyle().Foreground(creamC).Bold(true)
	creamText   = lipgloss.NewStyle().Foreground(creamC)
	mutedText   = lipgloss.NewStyle().Foreground(mutedC)
	dimText     = lipgloss.NewStyle().Foreground(dimgrayC).Italic(true)
	purpleText  = lipgloss.NewStyle().Foreground(purpleC).Italic(true)
	errText     = lipgloss.NewStyle().Foreground(redC).Bold(true)
	navyGold    = lipgloss.NewStyle().Background(navyC).Foreground(goldC).Bold(true)
	navySilver  = lipgloss.NewStyle().Background(navyC).Foreground(silverC)
	navyMuted   = lipgloss.NewStyle().Background(navyC).Foreground(mutedC)
	scrollTrack = lipgloss.NewStyle().Background(sBg)
	scrollThumb = lipgloss.NewStyle().Background(sFg)
)

type histEntry struct {
	kind, text string
	time       time.Time
	// tool panel fields (kind == "tool")
	toolName   string
	toolArgs   string
	toolResult string
	toolErr    bool
	expanded   bool
}

type tuiModel struct {
	ctrl                                                  *control.Controller
	cfg                                                   *config.Config
	sink                                                  *tuiSink
	modelName, sessionID                                  string
	inputBuf                                              strings.Builder
	history                                               []histEntry
	thinking                                              strings.Builder
	width, height                                         int
	quitting                                              bool
	errMsg                                                string
	startTime                                             time.Time
	totalTokens, ioTokens, ooTokens, toolCalls, turnCount int
	totalCost                                             float64
	spinnerIdx                                            int
	isThinking                                            bool
	currentTool                                           string
	scrollPos, maxScroll                                  int
	dragging                                              bool
	dragStartY, dragStartPos                              int
	asker                                                 *tuiAsker
	inputHistory                                          []string
	histIdx                                               int
}

func newTUIModel(cfg *config.Config, sid string) *tuiModel {
	return &tuiModel{cfg: cfg, modelName: cfg.DefaultModel, sessionID: sid, startTime: time.Now(), history: make([]histEntry, 0), asker: &tuiAsker{}, histIdx: 0}
}

func (m *tuiModel) Init() tea.Cmd { return tea.Batch(tickCmd(), tea.EnterAltScreen) }
func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

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
		m.isThinking = false
		m.thinking.Reset()
		n := len(m.history)
		if n > 0 && m.history[n-1].kind == "assist" {
			m.history[n-1].text += msg.text
		} else {
			m.history = append(m.history, histEntry{kind: "assist", text: msg.text})
		}
		return m, nil
	case agentReasoningMsg:
		m.thinking.WriteString(msg.text)
		return m, nil
	case agentToolMsg:
		m.toolCalls++
		m.currentTool = msg.name
		m.history = append(m.history, histEntry{kind: "tool", toolName: msg.name, toolArgs: msg.args, time: time.Now()})
		return m, nil
	case agentToolResultMsg:
		m.currentTool = ""
		for i := len(m.history) - 1; i >= 0; i-- {
			h := &m.history[i]
			if h.kind == "tool" && h.toolResult == "" && !h.toolErr {
				h.toolResult = trunc(msg.result, 4000)
				h.toolErr = msg.err
				break
			}
		}
		return m, nil
	case agentErrMsg:
		m.errMsg = msg.err.Error()
		m.isThinking = false
		return m, nil
	case agentTokensMsg:
		m.totalTokens += msg.total
		m.totalCost += msg.cost
		m.ioTokens += msg.input
		m.ooTokens += msg.output
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	w, h := m.width, m.height
	if w < 60 {
		w = 80
	}
	if h < 10 {
		h = 30
	}
	msgH := h - 5
	scrollW := 3
	scrollX := w - scrollW - 2

	if m.maxScroll > 0 {
		thumbH := msgH * msgH / (len(m.history) + msgH)
		if thumbH < 2 {
			thumbH = 2
		}
		thumbTop := 0
		if m.maxScroll > 0 {
			thumbTop = (msgH - thumbH) * (m.maxScroll - m.scrollPos) / m.maxScroll
		}
		if thumbTop < 0 {
			thumbTop = 0
		}
		if thumbTop+thumbH > msgH {
			thumbTop = msgH - thumbH
		}

		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollPos++
			if m.scrollPos > m.maxScroll {
				m.scrollPos = m.maxScroll
			}
		case tea.MouseButtonWheelDown:
			m.scrollPos--
			if m.scrollPos < 0 {
				m.scrollPos = 0
			}
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress {
				if msg.X >= scrollX && msg.X <= scrollX+scrollW {
					if msg.Y >= thumbTop && msg.Y <= thumbTop+thumbH {
						m.dragging = true
						m.dragStartY = msg.Y
						m.dragStartPos = m.scrollPos
					} else if msg.Y < thumbTop {
						m.scrollPos += 5
						if m.scrollPos > m.maxScroll {
							m.scrollPos = m.maxScroll
						}
					} else {
						m.scrollPos -= 5
						if m.scrollPos < 0 {
							m.scrollPos = 0
						}
					}
				}
			}
			if msg.Action == tea.MouseActionRelease {
				m.dragging = false
			}
		case tea.MouseButtonNone:
			if m.dragging {
				dy := msg.Y - m.dragStartY
				avail := msgH - thumbH
				if avail > 0 {
					step := float64(m.maxScroll) / float64(avail)
					m.scrollPos = m.dragStartPos - int(float64(dy)*step)
					if m.scrollPos < 0 {
						m.scrollPos = 0
					}
					if m.scrollPos > m.maxScroll {
						m.scrollPos = m.maxScroll
					}
				}
			}
		}
	}
	return m, nil
}

func (m *tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Permission dialog owns the keyboard while a question is pending.
	if m.asker != nil {
		if p := m.asker.Pending(); p != nil {
			if msg.Type == tea.KeyEsc {
				m.asker.Cancel()
				return m, nil
			}
			if len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
				m.asker.Answer(int(msg.Runes[0] - '1'))
			}
			return m, nil
		}
	}
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEsc:
		m.inputBuf.Reset()
		m.histIdx = len(m.inputHistory)
		return m, nil
	case tea.KeyPgUp:
		m.scrollPos += 10
		if m.scrollPos > m.maxScroll {
			m.scrollPos = m.maxScroll
		}
		return m, nil
	case tea.KeyPgDown:
		m.scrollPos -= 10
		if m.scrollPos < 0 {
			m.scrollPos = 0
		}
		return m, nil
	case tea.KeyHome:
		m.scrollPos = m.maxScroll
		return m, nil
	case tea.KeyEnd:
		m.scrollPos = 0
		return m, nil
	case tea.KeyUp:
		if msg.Alt {
			m.historyUp()
			return m, nil
		}
		m.scrollPos++
		if m.scrollPos > m.maxScroll {
			m.scrollPos = m.maxScroll
		}
		return m, nil
	case tea.KeyDown:
		if msg.Alt {
			m.historyDown()
			return m, nil
		}
		m.scrollPos--
		if m.scrollPos < 0 {
			m.scrollPos = 0
		}
		return m, nil
	case tea.KeyEnter:
		text := strings.TrimSpace(m.inputBuf.String())
		if text == "" {
			return m, nil
		}

		// Slash commands — interpreted client-side
		if strings.HasPrefix(text, "/") {
			handled, cmd := m.runSlash(text)
			if handled {
				m.inputBuf.Reset()
				m.histIdx = len(m.inputHistory)
				return m, cmd
			}
		}

		// Normal message — send to agent
		m.inputBuf.Reset()
		m.errMsg = ""
		m.thinking.Reset()
		m.isThinking = true
		m.currentTool = ""
		m.turnCount++
		m.scrollPos = 0
		m.history = append(m.history, histEntry{kind: "user", text: text})
		m.inputHistory = append(m.inputHistory, text)
		m.histIdx = len(m.inputHistory)
		return m, m.sendMessage(text)
	case tea.KeyBackspace:
		s := m.inputBuf.String()
		runes := []rune(s)
		if len(runes) > 0 {
			m.inputBuf.Reset()
			m.inputBuf.WriteString(string(runes[:len(runes)-1]))
		}
		return m, nil
	case tea.KeySpace:
		m.inputBuf.WriteRune(' ')
		m.histIdx = len(m.inputHistory)
		return m, nil
	default:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'e':
				m.toggleLastTool()
				return m, nil
			case 'E':
				m.toggleAllTools()
				return m, nil
			}
			m.inputBuf.WriteRune(msg.Runes[0])
			m.histIdx = len(m.inputHistory)
		}
		return m, nil
	}
}

// historyUp recalls the previous input (Alt+Up).
func (m *tuiModel) historyUp() {
	if len(m.inputHistory) == 0 {
		return
	}
	if m.histIdx > 0 {
		m.histIdx--
		m.setInput(m.inputHistory[m.histIdx])
	}
}

// historyDown moves forward through input history (Alt+Down).
func (m *tuiModel) historyDown() {
	if m.histIdx < len(m.inputHistory) {
		m.histIdx++
		if m.histIdx == len(m.inputHistory) {
			m.inputBuf.Reset()
			return
		}
		m.setInput(m.inputHistory[m.histIdx])
	}
}

func (m *tuiModel) setInput(s string) {
	m.inputBuf.Reset()
	m.inputBuf.WriteString(s)
}

// toggleLastTool expands/collapses the most recent tool panel (e).
func (m *tuiModel) toggleLastTool() {
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].kind == "tool" {
			m.history[i].expanded = !m.history[i].expanded
			return
		}
	}
}

// toggleAllTools expands or collapses every tool panel (E).
func (m *tuiModel) toggleAllTools() {
	anyExpanded := false
	for i := range m.history {
		if m.history[i].kind == "tool" && m.history[i].expanded {
			anyExpanded = true
		}
	}
	for i := range m.history {
		if m.history[i].kind == "tool" {
			m.history[i].expanded = !anyExpanded
		}
	}
}

func (m *tuiModel) View() string {
	if m.quitting {
		return accentBold.Render(fmt.Sprintf("\n  Goodbye · %d turns · %d tokens · %s\n\n", m.turnCount, m.totalTokens, time.Since(m.startTime).Round(time.Second)))
	}
	w, h := m.width, m.height
	if w < 60 {
		w = 80
	}
	if h < 10 {
		h = 30
	}
	scrollW := 3
	contentW := w - scrollW - 2
	msgH := h - 4
	if msgH < 3 {
		msgH = 3
	}

	var sb strings.Builder

	// Header
	sb.WriteString(accentText.Render("╭─⚕ Bounty" + strings.Repeat("─", contentW-10) + "╮\n"))
	if m.asker != nil {
		if ov := m.renderAskOverlay(contentW); ov != "" {
			sb.WriteString(ov)
		}
	}

	// Messages + scrollbar
	totalMsgs := len(m.history)
	m.maxScroll = totalMsgs - msgH
	if m.maxScroll < 0 {
		m.maxScroll = 0
	}
	if m.scrollPos > m.maxScroll {
		m.scrollPos = m.maxScroll
	}
	if m.scrollPos < 0 {
		m.scrollPos = 0
	}

	start := totalMsgs - msgH - m.scrollPos
	if start < 0 {
		start = 0
	}
	end := start + msgH
	if end > totalMsgs {
		end = totalMsgs
	}
	var visible []histEntry
	if start < end {
		visible = m.history[start:end]
	}

	thumbH, thumbTop := 0, 0
	if m.maxScroll > 0 {
		thumbH = msgH * msgH / (totalMsgs + msgH)
		if thumbH < 2 {
			thumbH = 2
		}
		thumbTop = (msgH - thumbH) * (m.maxScroll - m.scrollPos) / m.maxScroll
		if thumbTop < 0 {
			thumbTop = 0
		}
		if thumbTop+thumbH > msgH {
			thumbTop = msgH - thumbH
		}
	}

	var rendered []string
	for _, h := range visible {
		rendered = append(rendered, m.renderEntryLines(h, contentW)...)
	}

	idx := 0
	for i := 0; i < msgH; i++ {
		line := ""
		if i < msgH-len(rendered) {
			line = ""
		} else if idx < len(rendered) {
			line = rendered[idx]
			idx++
		}
		pad := contentW - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
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
		if m.currentTool != "" {
			ti += "  " + emojiFor(m.currentTool) + " " + m.currentTool
		}
		if m.thinking.Len() > 0 {
			t := m.thinking.String()
			if len(t) > 60 {
				t = t[len(t)-60:]
			}
			ti += "  " + t
		}
		sb.WriteString(dimText.Render("  "+ti) + "\n")
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
	if m.isThinking {
		sb.WriteString(spinnerFrames[m.spinnerIdx%len(spinnerFrames)])
	}
	sb.WriteString("\n")

	// Status bar
	elapsed := time.Since(m.startTime).Round(time.Second)
	left := navyGold.Render(" ⚕ Bounty ") + navySilver.Render(" "+m.modelName+" ")
	left += navyMuted.Render(fmt.Sprintf("│ %d tok │ turn %d ", m.ioTokens+m.ooTokens, m.turnCount))
	right := navyMuted.Render(" " + elapsed.String() + " ")
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	pad := w - lw - rw
	if pad < 1 {
		pad = 1
	}
	sb.WriteString(lipgloss.NewStyle().Background(navyC).Width(w).Render(left + strings.Repeat(" ", pad) + right))
	return sb.String()
}

func (m *tuiModel) renderEntryLines(h histEntry, w int) []string {
	if h.kind == "tool" {
		return m.renderToolLines(h, w)
	}
	return []string{m.renderLine(h, w)}
}

// renderToolLines renders a tool call as a collapsible panel: one summary line
// when collapsed; args, optional colored edit diff, and result when expanded.
func (m *tuiModel) renderToolLines(h histEntry, w int) []string {
	head := " 🔧 " + h.toolName
	if !h.expanded {
		if preview := toolPreview(h.toolArgs); preview != "" {
			head += "  " + trunc(preview, w-lipgloss.Width(head)-8)
		}
		switch {
		case h.toolErr:
			return []string{errText.Render(head + "  ✗")}
		case h.toolResult != "":
			return []string{dimText.Render(head + "  ✓")}
		default:
			return []string{dimText.Render(head + "  …")}
		}
	}

	var lines []string
	lines = append(lines, accentText.Render("┌ "+h.toolName+" "+emojiFor(h.toolName)))
	if h.toolArgs != "" {
		for _, l := range wrapArgs(h.toolArgs, w) {
			lines = append(lines, mutedText.Render("│ args: "+l))
		}
	}
	if h.toolName == "edit_file" || h.toolName == "multi_edit" {
		oldS, newS := editStrings(h.toolArgs)
		if oldS != "" || newS != "" {
			lines = append(lines, dimText.Render("│ diff:"))
			for _, l := range renderDiff(oldS, newS, w-4) {
				lines = append(lines, "│ "+l)
			}
		}
	}
	if h.toolResult != "" {
		for i, l := range strings.Split(h.toolResult, "\n") {
			if i >= 8 {
				lines = append(lines, mutedText.Render("│ …"))
				break
			}
			lines = append(lines, "│ "+trunc(l, w-4))
		}
	}
	status := "✓"
	if h.toolErr {
		status = "✗"
	}
	lines = append(lines, dimText.Render("└ "+status))
	return lines
}

// toolPreview extracts a short path/command preview from tool args so the
// collapsed line stays informative without leaking long payloads.
func toolPreview(args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	for _, k := range []string{"file_path", "path", "command", "pattern"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// wrapArgs truncates pretty-printed tool args to at most 2 lines.
func wrapArgs(args string, w int) []string {
	lines := strings.Split(args, "\n")
	if len(lines) > 2 {
		lines = lines[:2]
	}
	for i := range lines {
		lines[i] = trunc(lines[i], w-10)
	}
	return lines
}

// editStrings pulls old_string/new_string out of edit tool args.
func editStrings(args string) (string, string) {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return "", ""
	}
	oldS, _ := m["old_string"].(string)
	newS, _ := m["new_string"].(string)
	return oldS, newS
}

// renderAskOverlay draws the pending permission dialog.
func (m *tuiModel) renderAskOverlay(w int) string {
	p := m.asker.Pending()
	if p == nil {
		return ""
	}
	boxW := w - 8
	if boxW > 72 {
		boxW = 72
	}
	if boxW < 20 {
		boxW = 20
	}
	var sb strings.Builder
	sb.WriteString(navyGold.Render("┌─ 权限确认 ─"+strings.Repeat("─", boxW-11)+"┐") + "\n")
	for _, line := range []string{p.question} {
		sb.WriteString(navySilver.Render("│ "+trunc(line, boxW-4)) + "\n")
	}
	sb.WriteString(navySilver.Render("│") + "\n")
	for i, o := range p.options {
		sb.WriteString(navySilver.Render(fmt.Sprintf("│ %d. %s", i+1, trunc(o, boxW-8))) + "\n")
	}
	sb.WriteString(navyMuted.Render("└ 按数字键选择 · Esc 拒绝 ─"+strings.Repeat("─", boxW-22)+"┘") + "\n")
	return sb.String()
}

func (m *tuiModel) renderLine(h histEntry, w int) string {
	switch h.kind {
	case "user":
		return accentBold.Render("● ") + creamBold.Render(trunc(h.text, w-4))
	case "assist":
		return "    " + creamText.Render(trunc(h.text, w-6))
	case "thinking":
		return dimText.Render("┊ " + trunc(h.text, w-4))
	case "info":
		return mutedText.Render("  ℹ " + h.text)
	case "error":
		return errText.Render("  " + trunc(h.text, w-4))
	}
	return trunc(h.text, w)
}

// ---------------------------------------------------------------------------
// Session management (local slash commands)
// ---------------------------------------------------------------------------

func (m *tuiModel) switchSession(title string) {
	m.ctrl.SaveTurn()
	newID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	// Rebuild controller with the new session
	newCtrl, err := boot.RebuildSession(m.ctrl, m.cfg, newID, m.sink, m.asker)
	if err != nil {
		m.history = append(m.history, histEntry{kind: "error", text: fmt.Sprintf("Failed to create session: %v", err)})
		return
	}
	m.ctrl = newCtrl
	m.sessionID = newID
	m.history = nil
	m.turnCount = 0
	m.totalTokens = 0
	m.ioTokens = 0
	m.ooTokens = 0
	m.toolCalls = 0
	m.scrollPos = 0
	m.startTime = time.Now()
	m.history = append(m.history, histEntry{kind: "info", text: fmt.Sprintf("New session: %s (%s)", title, shortID(newID))})
}

func (m *tuiModel) loadSession(id string) {
	m.ctrl.SaveTurn()
	sessions, err := m.ctrl.ListSessions(100)
	if err != nil {
		m.history = append(m.history, histEntry{kind: "error", text: "Failed to list sessions: " + err.Error()})
		return
	}
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, id) || s.ID == id {
			// Rebuild controller with the loaded session
			newCtrl, buildErr := boot.RebuildSession(m.ctrl, m.cfg, s.ID, m.sink, m.asker)
			if buildErr != nil {
				m.history = append(m.history, histEntry{kind: "error", text: fmt.Sprintf("Failed to load session: %v", buildErr)})
				return
			}
			m.ctrl = newCtrl
			m.sessionID = s.ID
			m.history = nil
			m.turnCount = 0
			m.totalTokens = 0
			m.ioTokens = 0
			m.ooTokens = 0
			m.toolCalls = 0
			m.scrollPos = 0
			m.startTime = time.Now()
			// Replay loaded messages into history
			msgs, loadErr := m.ctrl.GetStore().LoadMessages(s.ID)
			if loadErr == nil {
				for _, msg := range msgs {
					switch msg.Role {
					case "user":
						m.history = append(m.history, histEntry{kind: "user", text: msg.Content})
						m.turnCount++
					case "assistant":
						m.history = append(m.history, histEntry{kind: "assist", text: msg.Content})
					}
				}
			}
			m.history = append(m.history, histEntry{kind: "info", text: fmt.Sprintf("Switched to: %s", s.Title)})
			return
		}
	}
	m.history = append(m.history, histEntry{kind: "error", text: "Session not found: " + id})
}

func (m *tuiModel) showSessionList() {
	sessions, err := m.ctrl.ListSessions(20)
	if err != nil {
		m.history = append(m.history, histEntry{kind: "error", text: "Failed to list sessions"})
		return
	}
	m.history = append(m.history, histEntry{kind: "info", text: "── Sessions ──"})
	for _, s := range sessions {
		marker := " "
		if s.ID == m.sessionID {
			marker = "*"
		}
		t := time.Unix(s.UpdatedAt, 0).Format("01-02 15:04")
		m.history = append(m.history, histEntry{kind: "info",
			text: fmt.Sprintf("%s %s  %s  %s", marker, shortID(s.ID), t, s.Title)})
	}
}

func (m *tuiModel) renameSession(title string) {
	st := m.ctrl.GetStore()
	sess, err := st.LoadSession(m.sessionID)
	if err == nil {
		sess.Title = title
		st.SaveSession(sess)
		m.history = append(m.history, histEntry{kind: "info", text: "Session renamed to: " + title})
	} else {
		m.history = append(m.history, histEntry{kind: "error", text: "Failed to rename session: " + err.Error()})
	}
}

func shortID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) >= 2 {
		return parts[len(parts)-1][:8]
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// runSlash dispatches client-side slash commands. Returns handled=false when
// the text is not a slash command (caller sends it as a normal message).
func (m *tuiModel) runSlash(text string) (bool, tea.Cmd) {
	cmd, arg, ok := parseSlash(text)
	if !ok {
		return false, nil
	}
	switch cmd {
	case "/new":
		title := arg
		if title == "" {
			title = "New Session"
		}
		m.switchSession(title)
		return true, nil
	case "/switch":
		m.loadSession(arg)
		return true, nil
	case "/list", "/sessions":
		m.showSessionList()
		return true, nil
	case "/rename":
		m.renameSession(arg)
		return true, nil
	case "/status":
		m.showStatus()
		return true, nil
	case "/model":
		m.switchModel(arg)
		return true, nil
	case "/compact":
		m.forceCompact()
		return true, nil
	case "/todo":
		m.showTodos()
		return true, nil
	case "/export":
		m.exportSession(arg)
		return true, nil
	case "/skills":
		m.showSkills()
		return true, nil
	case "/help":
		m.showHelp()
		return true, nil
	default:
		m.history = append(m.history, histEntry{kind: "error", text: "未知命令 " + cmd + "（/help 查看全部命令）"})
		return true, nil
	}
}

func parseSlash(text string) (string, string, bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	fields := strings.SplitN(text, " ", 2)
	cmd := fields[0]
	arg := ""
	if len(fields) == 2 {
		arg = strings.TrimSpace(fields[1])
	}
	return cmd, arg, true
}

func (m *tuiModel) showStatus() {
	m.history = append(m.history,
		histEntry{kind: "info", text: "── Status ──"},
		histEntry{kind: "info", text: fmt.Sprintf("session: %s", m.sessionID)},
		histEntry{kind: "info", text: fmt.Sprintf("model: %s", m.modelName)},
		histEntry{kind: "info", text: fmt.Sprintf("turns: %d · tool calls: %d · tokens: %d (in %d / out %d)", m.turnCount, m.toolCalls, m.totalTokens, m.ioTokens, m.ooTokens)},
		histEntry{kind: "info", text: fmt.Sprintf("uptime: %s", time.Since(m.startTime).Round(time.Second))},
	)
}

func (m *tuiModel) switchModel(spec string) {
	name, err := boot.SwitchModel(m.ctrl, m.cfg, spec)
	if err != nil {
		m.history = append(m.history, histEntry{kind: "error", text: "切换模型失败: " + err.Error()})
		return
	}
	m.modelName = name
	m.history = append(m.history, histEntry{kind: "info", text: "已切换模型: " + name})
}

func (m *tuiModel) forceCompact() {
	if err := m.ctrl.ForceCompact(); err != nil {
		m.history = append(m.history, histEntry{kind: "error", text: "压缩失败: " + err.Error()})
		return
	}
	m.history = append(m.history, histEntry{kind: "info", text: "上下文已立即压缩（保留最近消息 + 摘要）"})
}

func (m *tuiModel) showTodos() {
	todos, err := m.ctrl.GetStore().LoadTodos(m.sessionID)
	if err != nil || len(todos) == 0 {
		m.history = append(m.history, histEntry{kind: "info", text: "暂无待办事项"})
		return
	}
	m.history = append(m.history, histEntry{kind: "info", text: "── Todos ──"})
	for _, td := range todos {
		marker := " "
		switch td.Status {
		case "completed":
			marker = "x"
		case "in_progress":
			marker = ">"
		}
		m.history = append(m.history, histEntry{kind: "info", text: fmt.Sprintf("- [%s] %s", marker, td.Content)})
	}
}

func (m *tuiModel) exportSession(arg string) {
	p, err := exportSessionToFile(m.ctrl, m.sessionID, arg)
	if err != nil {
		m.history = append(m.history, histEntry{kind: "error", text: "导出失败: " + err.Error()})
		return
	}
	m.history = append(m.history, histEntry{kind: "info", text: "已导出: " + p})
}

func (m *tuiModel) showSkills() {
	entries := m.ctrl.Skills()
	if len(entries) == 0 {
		m.history = append(m.history, histEntry{kind: "info", text: "未发现已加载技能"})
		return
	}
	m.history = append(m.history, histEntry{kind: "info", text: fmt.Sprintf("── Skills (%d) ──", len(entries))})
	for _, e := range entries {
		marker := " "
		if e.IsSubagent {
			marker = "🤖"
		}
		m.history = append(m.history, histEntry{kind: "info", text: fmt.Sprintf("%s %s — %s", marker, e.Name, trunc(e.Description, 56))})
	}
}

func (m *tuiModel) showHelp() {
	m.history = append(m.history, histEntry{kind: "info", text: "── Slash 命令 ──"})
	for _, l := range []string{
		"/status — 会话/模型/token 状态",
		"/model provider/model — 切换配置内的模型（如 qwen/qwen3.8-max）",
		"/compact — 立即压缩上下文",
		"/todo — 当前待办列表",
		"/export [文件.md] — 导出会话为 Markdown",
		"/skills — 已加载技能列表",
		"/new · /switch ID · /list · /rename 标题 — 会话管理",
		"Alt+↑/↓ 输入历史 · e 展开最近工具 · E 全部展开/折叠",
	} {
		m.history = append(m.history, histEntry{kind: "info", text: "  " + l})
	}
}

func (m *tuiModel) sendMessage(text string) tea.Cmd {
	return func() tea.Msg {
		if err := m.ctrl.Send(context.Background(), text); err != nil {
			return agentErrMsg{err: err}
		}
		return agentTextMsg{text: ""}
	}
}

func emojiFor(n string) string {
	if e, ok := toolEmoji[n]; ok {
		return e
	}
	return "⚡"
}
func trunc(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

type agentTextMsg struct{ text string }
type agentReasoningMsg struct{ text string }
type agentToolMsg struct{ name, args string }
type agentToolResultMsg struct {
	result string
	err    bool
}
type agentErrMsg struct{ err error }
type agentTokensMsg struct {
	total, input, output int
	cost                 float64
}

type tuiSink struct{ program *tea.Program }

func (s *tuiSink) Emit(ev event.Event) {
	if s.program == nil {
		return
	}
	switch ev.Type {
	case "text":
		s.program.Send(agentTextMsg{text: ev.TextDelta})
	case "reasoning":
		s.program.Send(agentReasoningMsg{text: ev.ReasoningDelta})
	case "tool_call":
		s.program.Send(agentToolMsg{name: ev.ToolName, args: string(ev.ToolArgs)})
	case "tool_result":
		s.program.Send(agentToolResultMsg{result: ev.ToolResult, err: ev.ToolErr != ""})
	case "usage":
		t, i, o := 0, 0, 0
		if ev.Usage != nil {
			i = ev.Usage.InputTokens
			o = ev.Usage.OutputTokens
			t = i + o
		}
		c := float64(i)*0.28/1_000_000 + float64(o)*1.10/1_000_000
		s.program.Send(agentTokensMsg{total: t, input: i, output: o, cost: c})
	}
}

func RunTUI(cfg *config.Config, sid string) error {
	m := newTUIModel(cfg, sid)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	s := &tuiSink{program: p}
	c, err := boot.Build(cfg, boot.Options{MaxSteps: cfg.Agent.MaxSteps, Sink: s, Posture: permission.PostureAuto, SessionID: sid, Asker: m.asker})
	if err != nil {
		return err
	}
	m.ctrl = c
	m.sink = s
	_, err = p.Run()
	return err
}
