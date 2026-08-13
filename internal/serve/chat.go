package serve

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"bounty/internal/auth"
	"bounty/internal/checkpoint"
)

type ChatHandler struct {
	SendFn   func(text string) error
	SwitchFn func(req ModelSwitchRequest) error
	// CheckpointListFn/RestoreFn expose the git-shadow-repo rollback
	// (P3-3). nil means checkpoints are unavailable in this deployment.
	CheckpointListFn    func() ([]checkpoint.Info, error)
	CheckpointRestoreFn func(msgIndex int) error
}

// ModelSwitchRequest carries the connection parameters for a runtime model
// switch. The API key is kept in memory only and never persisted or logged.
type ModelSwitchRequest struct {
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Token auth check — duplicates the middleware so the handler stays safe
	// even if it is ever mounted without one.
	if want := os.Getenv("BOUNTY_AUTH_TOKEN"); want != "" {
		got := auth.TokenFromRequest(r)
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	if r.URL.Path == "/chat/api/send" && r.Method == "POST" {
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := h.SendFn(req.Message); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
		return
	}

	if r.URL.Path == "/chat/api/model" && r.Method == "POST" {
		var req ModelSwitchRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.TrimSpace(req.Model) == "" {
			http.Error(w, "model is required", 400)
			return
		}
		if h.SwitchFn == nil {
			http.Error(w, "model switching unavailable", 501)
			return
		}
		if err := h.SwitchFn(req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "model": req.Model})
		return
	}

	if r.URL.Path == "/chat/api/checkpoints" && r.Method == http.MethodGet {
		h.checkpointsAPI(w)
		return
	}
	if r.URL.Path == "/chat/api/checkpoints/restore" && r.Method == http.MethodPost {
		h.restoreAPI(w, r)
		return
	}

	// Serve the chat SPA
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(chatHTML))
}

// checkpointsAPI lists the per-message checkpoints of the current session.
func (h *ChatHandler) checkpointsAPI(w http.ResponseWriter) {
	if h.CheckpointListFn == nil {
		http.Error(w, "checkpoints unavailable", http.StatusNotImplemented)
		return
	}
	list, err := h.CheckpointListFn()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "checkpoints": list})
}

// restoreAPI rolls the workspace files back to just before the given user
// message (git shadow repo tag msg-<N>).
func (h *ChatHandler) restoreAPI(w http.ResponseWriter, r *http.Request) {
	if h.CheckpointRestoreFn == nil {
		http.Error(w, "checkpoints unavailable", http.StatusNotImplemented)
		return
	}
	var req struct {
		MsgIndex *int `json:"msg_index"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.MsgIndex == nil {
		http.Error(w, "msg_index is required", http.StatusBadRequest)
		return
	}
	if err := h.CheckpointRestoreFn(*req.MsgIndex); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "msg_index": *req.MsgIndex})
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
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }
#stats { font-size: 12px; color: var(--muted); padding: 4px 20px; background: var(--navy); display: flex; gap: 16px; }
#stats span { color: var(--gold); }
@media (max-width: 600px) { header { padding: 8px 12px; } main { padding: 8px 12px; } footer { padding: 8px 12px; } }
.tabs { display: flex; gap: 0; margin-bottom: 0; }
.tab { padding: 8px 20px; background: var(--surface); border: 1px solid var(--border); border-bottom: none; border-radius: 8px 8px 0 0; cursor: pointer; color: var(--muted); }
.tab.active { background: var(--bg); color: var(--gold); border-color: var(--gold); }
.panel { display: none; padding: 16px; border: 1px solid var(--border); border-radius: 0 8px 8px 8px; }
.panel.active { display: block; }
#panel-chat.panel.active { display: flex; flex-direction: column; flex: 1; padding: 0; border: none; border-radius: 0; }
#panel-chat.panel { display: none; }
#panel-devet.panel.active { display: block; }
#panel-devet { overflow-y: auto; max-height: calc(100vh - 120px); }
.devet-card { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 12px; margin: 8px 0; }
.devet-card h3 { color: var(--gold); margin: 0 0 8px 0; font-size: 14px; }
.attack-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 8px; }
.attack-item { background: var(--surface); border: 1px solid var(--border); border-radius: 6px; padding: 10px; font-size: 12px; }
.attack-item.detected { border-color: var(--green); }
.attack-item h4 { margin: 0; color: var(--cream); font-size: 13px; }
.attack-item .fault { color: var(--muted); font-size: 11px; }
select { background: var(--bg); color: var(--cream); border: 1px solid var(--border); border-radius: 4px; padding: 6px 10px; font-size: 13px; }
.result-ok { color: var(--green); font-weight: bold; }
.result-fail { color: var(--red); font-weight: bold; }
</style>
</head>
<body>
<header>
  <span class="dot" id="statusDot"></span>
  <h1>⚕ Bounty Chat Console</h1>
  <span class="status" id="statusText">connecting...</span>
</header>
<div id="model-bar" style="display:flex;gap:6px;padding:6px 20px;background:var(--surface);border-bottom:1px solid var(--border);align-items:center;font-size:12px;flex-wrap:wrap;">
  <span style="color:var(--muted);">模型:</span>
  <span id="currentModel" style="color:var(--gold);font-weight:600;">默认</span>
  <input id="modelUrl" placeholder="Base URL（不带 /v1，如 https://api.deepseek.com）" spellcheck="false" style="background:var(--bg);color:var(--cream);border:1px solid var(--border);border-radius:4px;padding:4px 8px;font-size:12px;width:220px;">
  <input id="modelKey" type="password" placeholder="API Key" style="background:var(--bg);color:var(--cream);border:1px solid var(--border);border-radius:4px;padding:4px 8px;font-size:12px;width:200px;">
  <input id="modelName" placeholder="模型名（如 deepseek-chat）" spellcheck="false" style="background:var(--bg);color:var(--cream);border:1px solid var(--border);border-radius:4px;padding:4px 8px;font-size:12px;width:180px;">
  <button id="modelSwitchBtn" onclick="switchModel()" style="background:var(--gold);color:#000;border:none;border-radius:4px;padding:4px 12px;font-weight:600;cursor:pointer;font-size:12px;">切换</button>
  <span id="modelResult" style="color:var(--muted);"></span>
</div>
<div class="tabs">
  <div class="tab tab-chat active" onclick="switchTab('chat')">💬 Chat</div>
  <div class="tab tab-devet" onclick="switchTab('devet')">🛡️ DeVET Security</div>
  <div class="tab tab-ckpt" onclick="switchTab('ckpt')">↩️ 回滚</div>
</div>
<div id="panel-chat" class="panel active">
<div id="stats">
  <div>Tokens: <span id="tokCount">0</span></div>
  <div>Turns: <span id="turnCount">0</span></div>
  <div>Tools: <span id="toolCount">0</span></div>
</div>
<div id="session-bar" style="display:flex;gap:8px;padding:4px 20px;background:var(--navy);align-items:center;font-size:12px;">
  <span id="sessionTitle" style="color:var(--gold);">New Session</span>
  <button onclick="newSession()" style="background:var(--surface);color:var(--cream);border:1px solid var(--border);border-radius:4px;padding:2px 8px;font-size:11px;cursor:pointer;">+ New</button>
  <button onclick="listSessions()" style="background:var(--surface);color:var(--cream);border:1px solid var(--border);border-radius:4px;padding:2px 8px;font-size:11px;cursor:pointer;">📋 List</button>
</div>
<main id="messages"></main>
<footer>
  <textarea id="input" rows="1" placeholder="Type a message... (Enter to send, Shift+Enter for newline)" onkeydown="handleKey(event)"></textarea>
  <button id="sendBtn" onclick="sendMessage()">Send</button>
</footer>
</div>
<div id="panel-devet" class="panel">
  <div class="devet-card">
    <h3>🔨 Scenario Builder</h3>
    <p style="color:var(--muted);font-size:13px;margin-bottom:8px;">Build and verify a Trading DAO delegation chain</p>
    <button onclick="buildChain()" style="background:var(--gold);color:#000;border:none;border-radius:4px;padding:8px 16px;font-weight:600;cursor:pointer;font-size:13px;">Build Trading DAO Chain</button>
    <div id="chain-result" style="margin-top:8px;font-size:13px;"></div>
  </div>
  <div class="devet-card">
    <h3>🔍 Chain Verification</h3>
    <div id="verify-result" style="font-size:13px;">
      <p style="color:var(--muted);">Run Scenario Builder first to see verification results.</p>
    </div>
  </div>
  <div class="devet-card">
    <h3>⚔️ Attack Simulator</h3>
    <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;">
      <select id="attack-select">
        <option value="">-- Select Attack Type --</option>
        <option value="A1_delegation_replacement">A1: 委托替换</option>
        <option value="A2_sub_result_forgery">A2: 子结果伪造</option>
        <option value="A3_depth_violation">A3: 委托深度违规</option>
        <option value="A4_api_overrun">A4: API 调用超限</option>
        <option value="A6_expired_grant">A6: 委托过期</option>
        <option value="A7_grant_tampering">A7: Grant 篡改</option>
        <option value="A8_collusion">A8: 跨 Host 共谋</option>
        <option value="A10_replayed_grant">A10: 重放攻击</option>
      </select>
      <button onclick="simulateAttack()" style="background:var(--gold);color:#000;border:none;border-radius:4px;padding:8px 16px;font-weight:600;cursor:pointer;font-size:13px;">Simulate</button>
    </div>
    <div id="sim-result" style="margin-top:8px;font-size:13px;"></div>
  </div>
  <div class="devet-card">
    <h3>📊 Attack Summary</h3>
    <div class="attack-grid" id="attack-grid">
    </div>
  </div>
</div>
<div id="panel-ckpt" class="panel">
  <div class="devet-card">
    <h3>↩️ 检查点回滚（git 影子仓库）</h3>
    <p style="color:var(--muted);font-size:13px;margin-bottom:8px;">每条用户消息开始前自动生成一个工作区全量快照。回滚会把工作区文件恢复到该消息开始前的状态，并清除之后新增的文件（.git 目录不受影响）。</p>
    <div style="display:flex;gap:8px;align-items:center;margin-bottom:8px;">
      <button onclick="refreshCheckpoints()" style="background:var(--gold);color:#000;border:none;border-radius:4px;padding:6px 14px;font-weight:600;cursor:pointer;font-size:13px;">🔄 刷新检查点</button>
    </div>
    <div id="ckpt-list" style="font-size:13px;"></div>
  </div>
</div>
<script>
let turnCount = 0, tokCount = 0, toolCount = 0;
const msgEl = document.getElementById('messages');
const inputEl = document.getElementById('input');
const sendBtn = document.getElementById('sendBtn');
const statusDot = document.getElementById('statusDot');
const statusText = document.getElementById('statusText');
let currentBotMsg = null;
let es = null;

const urlParams = new URLSearchParams(window.location.search);
const authToken = urlParams.get('token') || '';
const tokenParam = authToken ? ('?token=' + authToken) : '';

function connectSSE() {
  es = new EventSource('/events' + tokenParam);
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
  if (sendBtn.disabled) return;
  const text = inputEl.value.trim();
  if (!text) return;
  addMsg('user', text);
  inputEl.value = '';
  turnCount++;
  document.getElementById('turnCount').textContent = turnCount;
  sendBtn.disabled = true;

  fetch('/chat/api/send' + tokenParam, {
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

// ── Checkpoint Rollback (git shadow repo) ──
async function refreshCheckpoints() {
  const el = document.getElementById('ckpt-list');
  if (!el) return;
  el.innerHTML = '<span style="color:var(--muted);">加载中...</span>';
  try {
    const r = await fetch('/chat/api/checkpoints' + tokenParam);
    const d = await r.json();
    if (!r.ok || d.status !== 'ok') {
      el.innerHTML = '<span class="result-fail">❌ ' + esc(d.error || '检查点不可用') + '</span>';
      return;
    }
    const list = (d.checkpoints || []).slice().reverse();
    if (!list.length) {
      el.innerHTML = '<span style="color:var(--muted);">暂无检查点。发送第一条消息后，每条消息前会自动生成一个快照。</span>';
      return;
    }
    let html = '';
    list.forEach(c => {
      html += '<div style="display:flex;justify-content:space-between;align-items:flex-start;gap:8px;padding:8px 0;border-bottom:1px solid var(--border);">'
        + '<div><span style="color:var(--gold);font-weight:600;">消息 #' + esc(c.msg_index) + '</span>'
        + (c.prompt ? '<div style="color:var(--muted);font-size:12px;white-space:pre-wrap;word-break:break-word;">' + esc(c.prompt) + '</div>' : '')
        + '</div>'
        + '<button onclick="restoreCheckpoint(' + Number(c.msg_index) + ')" style="background:var(--surface);color:var(--red);border:1px solid var(--border);border-radius:4px;padding:4px 10px;font-size:12px;cursor:pointer;white-space:nowrap;">回滚到此</button>'
        + '</div>';
    });
    el.innerHTML = html;
  } catch (err) {
    el.innerHTML = '<span class="result-fail">❌ ' + esc(err) + '</span>';
  }
}

async function restoreCheckpoint(idx) {
  if (!confirm('确认回滚工作区到消息 #' + idx + ' 开始前的状态？之后新增/修改的文件将被还原或清除。')) return;
  const el = document.getElementById('ckpt-list');
  el.innerHTML = '<span style="color:var(--gold);">⏳ 回滚中...</span>';
  try {
    const r = await fetch('/chat/api/checkpoints/restore' + tokenParam, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({msg_index: idx})
    });
    const d = await r.json();
    if (r.ok && d.status === 'ok') {
      el.innerHTML = '<span class="result-ok">✅ 已回滚到消息 #' + Number(idx) + ' 开始前的状态</span>';
    } else {
      el.innerHTML = '<span class="result-fail">❌ ' + esc(d.error || '回滚失败') + '</span>';
    }
  } catch (err) {
    el.innerHTML = '<span class="result-fail">❌ ' + esc(err) + '</span>';
  }
}

// ── Model Switching ──
function switchModel() {
  const urlEl = document.getElementById('modelUrl');
  const keyEl = document.getElementById('modelKey');
  const nameEl = document.getElementById('modelName');
  const resultEl = document.getElementById('modelResult');
  const btn = document.getElementById('modelSwitchBtn');
  const model = nameEl.value.trim();
  if (!model) { resultEl.textContent = '请填写模型名'; return; }
  btn.disabled = true;
  resultEl.textContent = '⏳ 测试连接并切换...';
  fetch('/chat/api/model' + tokenParam, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({kind: 'openai', base_url: urlEl.value.trim(), api_key: keyEl.value.trim(), model: model})
  }).then(r => r.json().then(d => ({ok: r.ok, d}))).then(({ok, d}) => {
    btn.disabled = false;
    if (ok && d.status === 'ok') {
      document.getElementById('currentModel').textContent = d.model;
      resultEl.textContent = '✅ 已切换';
    } else {
      resultEl.textContent = '❌ ' + (d.error || '切换失败');
    }
  }).catch(err => {
    btn.disabled = false;
    resultEl.textContent = '❌ ' + err;
  });
}

// ── Session Management ──
function newSession() {
    fetch('/chat/api/send' + tokenParam, {method:'POST',headers:{'Content-Type':'application/json'},
        body:JSON.stringify({message:'/new'})}).then(() => location.reload());
}
function listSessions() {
    window.open('/dashboard', '_blank');
}

// ── Tab Switching ──
function switchTab(name) {
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.querySelectorAll('.panel').forEach(p => p.classList.remove('active'));
  document.querySelector('.tab-' + name).classList.add('active');
  document.getElementById('panel-' + name).classList.add('active');
  if (name === 'ckpt') refreshCheckpoints();
}

// ── sendMessageText: post a message directly without using the textarea ──
function sendMessageText(text) {
  if (sendBtn.disabled) return;
  addMsg('user', text);
  turnCount++;
  document.getElementById('turnCount').textContent = turnCount;
  sendBtn.disabled = true;
  fetch('/chat/api/send' + tokenParam, {
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

// HTML-escape untrusted values before any innerHTML interpolation.
function esc(s) {
  return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}

// ── DeVET API Helper ──
function devetAction(action, body) {
  switchTab('chat');
  sendMessageText(body || action);
}

// ── Scenario Builder ──
async function buildChain() {
  const el = document.getElementById('chain-result');
  el.innerHTML = '<span style="color:var(--gold);">⏳ Building delegation chain...</span>';
  try {
    devetAction('build', '请构建一个DeVET委托链');
    el.innerHTML = '<span class="result-ok">✅ Chain build sent. See Chat tab for results.</span>';
  } catch(err) {
    el.innerHTML = '<span class="result-fail">❌ Build failed: ' + esc(err) + '</span>';
  }
}

// ── Attack Simulator ──
async function simulateAttack() {
  const sel = document.getElementById('attack-select');
  const attack = sel.value;
  if (!attack) return;
  const el = document.getElementById('sim-result');
  el.innerHTML = '<span style="color:var(--gold);">⏳ Simulating ' + esc(attack) + '...</span>';
  try {
    devetAction('simulate', '请模拟DeVET攻击: ' + attack);
    el.innerHTML = '<span class="result-ok">✅ Attack simulation sent. See Chat tab for full trace.</span>';
  } catch(err) {
    el.innerHTML = '<span class="result-fail">❌ Simulation failed: ' + esc(err) + '</span>';
  }
}

// ── Populate Attack Summary Grid ──
(function() {
  const attacks = [
    {id:'A1_delegation_replacement', name:'A1: 委托替换', fault:'grant_tampered', desc:'Grant被篡改'},
    {id:'A2_sub_result_forgery', name:'A2: 子结果伪造', fault:'subagent_proof_invalid', desc:'子代理证明无效'},
    {id:'A3_depth_violation', name:'A3: 委托深度违规', fault:'policy_violation', desc:'策略违规'},
    {id:'A4_api_overrun', name:'A4: API 调用超限', fault:'policy_violation', desc:'策略违规'},
    {id:'A6_expired_grant', name:'A6: 委托过期', fault:'expired', desc:'委托已过期'},
    {id:'A7_grant_tampering', name:'A7: Grant 篡改', fault:'grant_tampered', desc:'Grant被篡改'},
    {id:'A8_collusion', name:'A8: 跨 Host 共谋', fault:'subagent_proof_invalid', desc:'子代理证明无效'},
    {id:'A10_replayed_grant', name:'A10: 重放攻击', fault:'expired', desc:'委托已过期'}
  ];
  const grid = document.getElementById('attack-grid');
  if (!grid) return;
  attacks.forEach(a => {
    const div = document.createElement('div');
    div.className = 'attack-item detected';
    div.innerHTML = '<h4>✅ ' + esc(a.name) + '</h4><div class="fault">Fault: ' + esc(a.fault) + '</div><div style="font-size:10px;color:var(--muted);margin-top:2px;">' + esc(a.desc) + '</div>';
    grid.appendChild(div);
  });
})();

</script>
</body>
</html>`
}
