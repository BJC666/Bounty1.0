package serve

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

// DashboardHandler serves the web dashboard.
type DashboardHandler struct {
	SessionList func() []SessionInfo
}

// SessionInfo is the dashboard-facing view of a session.
type SessionInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Model     string `json:"model"`
	UpdatedAt int64  `json:"updated_at"`
	Turns     int    `json:"turns"`
}

func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/dashboard")
	if path == "" || path == "/" {
		h.index(w, r)
		return
	}
	if path == "/api/sessions" {
		h.apiSessions(w, r)
		return
	}
	http.NotFound(w, r)
}

func (h *DashboardHandler) index(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Bounty Dashboard</title>
    <style>
        :root { --bg: #1a1a2e; --card: #16213e; --accent: #0f3460; --text: #e0e0e0; --green: #4CAF50; --yellow: #FF9800; }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: system-ui, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; }
        header { background: var(--card); padding: 16px 24px; border-bottom: 1px solid var(--accent); display: flex; justify-content: space-between; align-items: center; }
        .status-dot { width: 10px; height: 10px; border-radius: 50%; background: var(--green); display: inline-block; margin-right: 8px; }
        main { max-width: 1200px; margin: 0 auto; padding: 24px; }
        .card { background: var(--card); border-radius: 8px; padding: 16px; margin-bottom: 16px; border: 1px solid var(--accent); }
        .card h3 { margin-bottom: 8px; color: var(--green); }
        .event-log { height: 400px; overflow-y: auto; background: #0a0a1a; border-radius: 4px; padding: 12px; font-family: monospace; font-size: 13px; line-height: 1.6; }
        .event-reasoning { color: #888; }
        .event-text { color: #e0e0e0; }
        .event-tool { color: var(--yellow); }
        .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
        @media (max-width: 768px) { .grid { grid-template-columns: 1fr; } }
    </style>
</head>
<body>
    <header>
        <h1><span class="status-dot"></span>Bounty Dashboard</h1>
        <div id="stats">Connecting...</div>
    </header>
    <main>
        <div class="grid">
            <div>
                <div class="card">
                    <h3>Sessions</h3>
                    <div id="sessions">Loading...</div>
                </div>
                <div class="card">
                    <h3>System</h3>
                    <div id="system">Loading...</div>
                </div>
            </div>
            <div>
                <div class="card">
                    <h3>Live Events</h3>
                    <div id="events" class="event-log"></div>
                </div>
            </div>
        </div>
    </main>
    <script>
        // Load sessions
        fetch('/dashboard/api/sessions').then(r => r.json()).then(data => {
            document.getElementById('sessions').innerHTML = data.sessions.map(s =>
                '<div style="padding:8px;border-bottom:1px solid #333"><strong>'+s.title+'</strong><br><small>'+s.model+' · '+s.turns+' turns</small></div>'
            ).join('') || '<p>No sessions</p>';
        });

        // SSE event stream
        const eventsEl = document.getElementById('events');
        const es = new EventSource('/events');
        es.onmessage = (e) => {
            const ev = JSON.parse(e.data);
            let cls = 'event-text';
            if (ev.Type === 'reasoning') cls = 'event-reasoning';
            if (ev.Type === 'tool_call') cls = 'event-tool';
            eventsEl.innerHTML += '<span class="'+cls+'">'+(ev.TextDelta||'')+(ev.ReasoningDelta||'')+(ev.ToolName?' ['+ev.ToolName+']':'')+'</span>';
            eventsEl.scrollTop = eventsEl.scrollHeight;
            if (eventsEl.children.length > 200) eventsEl.removeChild(eventsEl.firstChild);
        };
        es.onopen = () => document.querySelector('.status-dot').style.background = 'var(--green)';
        es.onerror = () => document.querySelector('.status-dot').style.background = 'var(--yellow)';
    </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, _ := template.New("dashboard").Parse(html)
	tmpl.Execute(w, nil)
}

func (h *DashboardHandler) apiSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.SessionList()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sessions": sessions})
}
