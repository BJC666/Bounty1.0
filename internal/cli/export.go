package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bounty/internal/control"
	"bounty/internal/store"
)

// buildSessionMarkdown renders a session as a Markdown transcript, mirroring
// the web export format.
func buildSessionMarkdown(sess *store.Session, msgs []store.Message) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", sess.Title))
	sb.WriteString(fmt.Sprintf("Model: %s | Provider: %s | %s\n\n", sess.Model, sess.Provider, time.Unix(sess.CreatedAt, 0).Format("2006-01-02 15:04")))
	sb.WriteString("---\n\n")
	for _, m := range msgs {
		switch m.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("**You:** %s\n\n", m.Content))
		case "assistant":
			sb.WriteString(fmt.Sprintf("**Bounty:** %s\n\n", m.Content))
		case "tool":
			sb.WriteString(fmt.Sprintf("*🔧 %s:* %s\n\n", m.ToolName, truncateMD(m.Content, 200)))
		}
	}
	return sb.String()
}

func truncateMD(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

var unsafeFileChars = regexp.MustCompile(`[\\/:*?"<>|]`)

// exportSessionToFile writes the session transcript to <arg> (or
// "<session-title>.md" when arg is empty) in the current directory and returns
// the absolute path written.
func exportSessionToFile(ctrl *control.Controller, sessionID, arg string) (string, error) {
	st := ctrl.GetStore()
	sess, err := st.LoadSession(sessionID)
	if err != nil {
		return "", fmt.Errorf("load session: %w", err)
	}
	msgs, err := st.LoadMessages(sessionID)
	if err != nil {
		return "", fmt.Errorf("load messages: %w", err)
	}
	md := buildSessionMarkdown(sess, msgs)

	name := strings.TrimSpace(arg)
	if name == "" {
		title := unsafeFileChars.ReplaceAllString(sess.Title, "_")
		if strings.TrimSpace(title) == "" {
			title = sessionID
		}
		name = title + ".md"
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(md), 0644); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	return abs, nil
}
