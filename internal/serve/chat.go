package serve

import (
	"encoding/json"
	"net/http"
)

type ChatHandler struct {
	SendFn func(text string) error
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/chat/api/send" && r.Method == "POST" {
		var req struct{ Message string `json:"message"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400); return
		}
		if err := h.SendFn(req.Message); err != nil {
			http.Error(w, err.Error(), 500); return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
		return
	}

	// Serve the chat SPA
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(chatHTML))
}

var chatHTML string

func init() {
	chatHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Bounty Chat Console</title>
<style>
:root {
  --bg: #0d1117; --surface: #161b22; --border: #30363d;
  --gold: #FFD700; --cream: #FFF8DC; --accent: #58a6ff;
  --green: #3fb950; --red: #f85149; --purple: #a371f7; --muted: #8b949e;
  --navy: #1a1a2e; --radius: 8px;
}
* { margin:0; padding:0; box-sizing:border-box; }
body { font-family: system-ui, -apple-system, sans-serif; background: var(--bg); color: var(--cream); height: 100vh; display: flex; flex-direction: column; }
header {
  background: var(--surface); border-bottom: 1px solid var(--border);
  padding: 10px 20px; display: flex; align-items: center; gap: 12px;
}
header h1 { font-size: 16px; color: var(--gold); }
header .status { font-size: 12px; color: var(--muted); }
header .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--green); display: inline-block; }
main { flex: 1; overflow-y: auto; padding: 16px 20px; display: flex; flex-direction: column; gap: 8px; }
footer { background: var(--surface); border-top: 1px solid var(--border); padding: 12px 20px; display: flex; gap: 8px; }
footer textarea {
  flex: 1; background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius);
  color: var(--cream); padding: 10px 14px; font-size: 14px; font-family: inherit;
  resize: none; outline: none; min-height: 44px; max-height: 120px;
}
footer textarea:focus { border-color: var(--gold); }
footer button {
  background: var(--gold); color: #000; border: none; border-radius: var(--radius);
  padding: 10px 20px; font-size: 14px; font-weight: 600; cursor: pointer;
}
footer button:hover { opacity: 0.9; }
footer button:disabled { opacity: 0.5; cursor: not-allowed; }
.msg { padding: 8px 0; animation: fadeIn 0.2s; }
@keyframes fadeIn { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: translateY(0); } }
.msg-user { color: var(--green); font-weight: 600; }
.msg-user::before { content: "● "; color: var(--gold); }
.msg-bot { color: var(--cream); padding-left: 16px; white-space: pre-wrap; word-break: break-word; }
.msg-tool { color: var(--muted); padding-left: 24px; font-style: italic; font-size: 13px; }
.msg-error { color: var(--red); padding-left: 16px; font-weight: 600; }
.msg-thinking { color: var(--purple); padding-left: 16px; font-style: italic; font-size: 13px; }
.tool-dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; margin-right: 6px; }
.tool-running { background: var(--gold); animation: pulse 0.8s infinite; }
.tool-done { background: var(--green); }
.tool-err { background: var(--red); }
@keyframes pulse { 0%%, 100%% { opacity: 1; } 50% { opacity: 0.3; } }
#stats { font-size: 12px; color: var(--muted); padding: 4px 20px; background: var(--navy); display: flex; gap: 16px; }
#stats span { color: var(--gold); }
@media (max-width: 600px) { header { padding: 8px 12px; } main { padding: 8px 12px; } footer { padding: 8px 12px; } }
</style>
</head>
<body>
<header>
  <span class="dot" id="statusDot"></span>
  <h1>⚕ Bounty Chat Console</h1>
  <span class="status" id="statusText">connecting...</span>
</header>
<div id="stats">
  <div>Tokens: <span id="tokCount">0</span></div>
  <div>Turns: <span id="turnCount">0</span></div>
  <div>Tools: <span id="toolCount">0</span></div>
</div>
<main id="messages"></main>
<footer>
  <textarea id="input" rows="1" placeholder="Type a message... (Enter to send, Shift+Enter for newline)" onkeydown="handleKey(event)"></textarea>
  <button id="sendBtn" onclick="sendMessage()">Send</button>
</footer>
<script>
let turnCount = 0, tokCount = 0, toolCount = 0;
const msgEl = document.getElementById('messages');
const inputEl = document.getElementById('input');
const sendBtn = document.getElementById('sendBtn');
const statusDot = document.getElementById('statusDot');
const statusText = document.getElementById('statusText');
let currentBotMsg = null;
let es = null;

function connectSSE() {
  es = new EventSource('/events');
  es.onopen = () => {
    statusDot.style.background = 'var(--green)';
    statusText.textContent = 'connected';
  };
  es.onerror = () => {
    statusDot.style.background = 'var(--red)';
    statusText.textContent = 'disconnected';
    setTimeout(connectSSE, 3000);
  };
  es.onmessage = (e) => {
    try {
      const ev = JSON.parse(e.data);
      handleEvent(ev);
    } catch(err) {}
  };
}
connectSSE();

function handleEvent(ev) {
  switch(ev.Type) {
    case 'text':
      if (!currentBotMsg) {
        currentBotMsg = addMsg('bot', '');
      }
      currentBotMsg.textContent += ev.TextDelta || '';
      msgEl.scrollTop = msgEl.scrollHeight;
      break;
    case 'reasoning':
      // show as thinking indicator
      break;
    case 'tool_call':
      toolCount++;
      document.getElementById('toolCount').textContent = toolCount;
      addMsg('tool', '🔧 ' + (ev.ToolName || 'tool') + '...');
      break;
    case 'tool_result':
      if (ev.ToolErr) {
        addMsg('error', '❌ ' + (ev.ToolName||'') + ': ' + ev.ToolErr);
      } else {
        addMsg('tool', '✅ done');
      }
      break;
    case 'usage':
      if (ev.Usage) {
        tokCount += (ev.Usage.InputTokens||0) + (ev.Usage.OutputTokens||0);
        document.getElementById('tokCount').textContent = tokCount;
      }
      break;
    case 'turn_complete':
      currentBotMsg = null;
      break;
  }
}

function addMsg(kind, text) {
  const div = document.createElement('div');
  div.className = 'msg msg-' + kind;
  if (kind === 'user') {
    div.textContent = text;
  } else if (kind === 'bot') {
    div.textContent = text;
  } else {
    div.textContent = text;
  }
  msgEl.appendChild(div);
  msgEl.scrollTop = msgEl.scrollHeight;
  return div;
}

function sendMessage() {
  const text = inputEl.value.trim();
  if (!text) return;
  addMsg('user', text);
  inputEl.value = '';
  turnCount++;
  document.getElementById('turnCount').textContent = turnCount;
  sendBtn.disabled = true;

  fetch('/chat/api/send', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({message: text})
  }).then(r => r.json()).then(data => {
    sendBtn.disabled = false;
  }).catch(err => {
    addMsg('error', 'Send failed: ' + err);
    sendBtn.disabled = false;
  });
}

function handleKey(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    sendMessage();
  }
}
</script>
</body>
</html>`
}
