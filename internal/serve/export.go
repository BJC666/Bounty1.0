package serve

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"bounty/internal/store"
)

type ExportHandler struct {
	LoadSessionFn  func(id string) (*store.Session, error)
	LoadMessagesFn func(sessionID string) ([]store.Message, error)
}

func (h *ExportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "md"
	}
	if sessionID == "" {
		http.Error(w, "missing ?session=ID", 400)
		return
	}

	sess, err := h.LoadSessionFn(sessionID)
	if err != nil {
		http.Error(w, "session not found", 404)
		return
	}
	msgs, err := h.LoadMessagesFn(sessionID)
	if err != nil {
		http.Error(w, "messages not found", 404)
		return
	}

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session": sess, "messages": msgs,
		})
	case "html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(exportHTML(sess, msgs)))
	default:
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write([]byte(exportMarkdown(sess, msgs)))
	}
}

func exportMarkdown(sess *store.Session, msgs []store.Message) string {
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
			sb.WriteString(fmt.Sprintf("*🔧 %s:* %s\n\n", m.ToolName, truncate(m.Content, 200)))
		}
	}
	return sb.String()
}

func exportHTML(sess *store.Session, msgs []store.Message) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"><title>Bounty Session</title>
<style>body{font-family:system-ui;max-width:800px;margin:0 auto;padding:20px;background:#0d1117;color:#e6edf3}
.user{color:#3fb950;font-weight:bold}.assistant{color:#e6edf3;margin-left:16px}.tool{color:#8b949e;margin-left:32px;font-style:italic}
</style></head><body>`)
	sb.WriteString(fmt.Sprintf("<h1>%s</h1><p>%s | %s</p><hr>", html.EscapeString(sess.Title), html.EscapeString(sess.Model), time.Unix(sess.CreatedAt, 0).Format("2006-01-02 15:04")))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("<div class='user'><strong>You:</strong> %s</div>", html.EscapeString(m.Content)))
		case "assistant":
			sb.WriteString(fmt.Sprintf("<div class='assistant'><strong>Bounty:</strong> %s</div>", html.EscapeString(m.Content)))
		case "tool":
			sb.WriteString(fmt.Sprintf("<div class='tool'>🔧 %s: %s</div>", html.EscapeString(m.ToolName), html.EscapeString(truncate(m.Content, 200))))
		}
	}
	sb.WriteString("</body></html>")
	return sb.String()
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
