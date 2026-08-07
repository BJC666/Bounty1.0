# Bounty Agent — 使用指南

> 通用 AI 智能体框架 · Go 单二进制 · 多 Provider · 自改进闭环

## 目录

1. [安装](#1-安装)
2. [快速开始](#2-快速开始)
3. [配置](#3-配置)
4. [CLI 命令](#4-cli-命令)
5. [Provider 设置](#5-provider-设置)
6. [工具参考](#6-工具参考)
7. [权限系统](#7-权限系统)
8. [Hook 系统](#8-hook-系统)
9. [技能系统](#9-技能系统)
10. [通道系统](#10-通道系统)
11. [子代理](#11-子代理)
12. [记忆系统](#12-记忆系统)
13. [上下文管理](#13-上下文管理)
14. [自改进](#14-自改进)
15. [MCP 集成](#15-mcp-集成)
16. [部署](#16-部署)
17. [开发指南](#17-开发指南)

---

## 1. 安装

### 从源码构建

```bash
git clone https://github.com/bounty/bounty.git
cd bounty
make build
# 输出: ./bounty (单二进制)
```

### Docker

```bash
docker build -t bounty-agent .
docker run -it --rm -v $(pwd):/workspace -e DEEPSEEK_API_KEY=$DEEPSEEK_API_KEY bounty-agent chat
```

### Homebrew (macOS/Linux)

```bash
brew install bounty
```

### npm

```bash
npm install -g bounty-agent
bounty chat
```

---

## 2. 快速开始

### 第一步：设置 API Key

```bash
# DeepSeek
export DEEPSEEK_API_KEY="sk-your-key-here"

# 或 Anthropic
export ANTHROPIC_API_KEY="sk-ant-your-key-here"

# 多 Key 轮转（用逗号分隔）
export DEEPSEEK_API_KEY="key1,key2,key3"
```

### 第二步：创建项目配置

```bash
cd your-project/
bounty doctor
```

这会在当前目录生成 `bounty.toml` 示例配置。

### 第三步：开始对话

```bash
bounty chat
```

```
 Bounty Agent  Ctrl+C to quit

> 帮我分析这个项目的架构

 Bounty 会调用 read_file、grep、glob 等工具来分析代码，
 流式输出分析结果。
```

---

## 3. 配置

### bounty.toml 完整示例

```toml
config_version = 1
default_model = "deepseek/deepseek-v4-pro"

# ── Provider 配置 ──
[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"
context_window = 1000000

[[providers]]
name = "anthropic"
kind = "anthropic"
models = ["claude-sonnet-5", "claude-opus-4-8"]
api_key_env = "ANTHROPIC_API_KEY"
context_window = 200000

[[providers]]
name = "ollama"
kind = "ollama"
base_url = "http://localhost:11434"
models = ["llama3.1:8b", "qwen2.5:14b"]

# ── Agent 行为 ──
[agent]
temperature = 0.0
compact_ratio = 0.8          # 上下文80%时触发压缩
compact_force_ratio = 0.9    # 90%时强制压缩
soft_compact_ratio = 0.5     # 50%时提示
max_subagent_depth = 2       # 子代理最大深度
max_subagent_concurrency = 6 # 并行子代理上限
max_parallel_writers = 3     # 并行写者上限
max_steps = 50               # 单轮最大工具调用步数

# ── 沙箱 ──
[sandbox]
workspace_root = ""
bash = "enforce"             # enforce | off
network = true

# ── 权限 ──
[permissions.allow]
tools = ["Read", "Glob", "Grep", "WebSearch", "WebFetch", "Edit", "Write"]
bash_patterns = [
    "ls *", "cat *", "pwd", "echo *",
    "git status *", "git diff *", "git add *", "git commit *",
    "go build *", "go test *", "python *",
    "mkdir *", "touch *", "cp *", "mv *", "curl *",
    "*"
]

[permissions.deny]
bash_patterns = [
    "rm -rf *", "rm *", "sudo *",
    "git push --force *", "git reset --hard *",
    "shutdown *", "reboot *",
    "chmod 777 *", "docker rm *"
]
forbid_write = [
    "Windows/*", "Program Files/*",
    "/etc/*", "~/.ssh/*"
]

# ── Hook ──
[hooks]
enabled = false

[[hooks.shell]]
event = "PreToolUse"
matcher = "bash"
command = "bash-validator.sh"
timeout_seconds = 30

# ── 远程 ──
[remote]
# enabled = false
# host = "myserver.example.com"
# user = "deploy"
# key = "~/.ssh/id_ed25519"

# ── MCP 插件 ──
[[plugins]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp@latest"]
```

### 配置优先级

```
命令行 --model deepseek/deepseek-v4-pro   ← 最高
  ↓
./bounty.toml                              ← 项目配置
  ↓
~/.config/bounty/config.toml               ← 用户配置
  ↓
内置默认值                                    ← 最低
```

### 配置快照修复

如果 `bounty.toml` 损坏导致启动失败，自动回退到最后已知良好快照：

```
Config load failed: invalid TOML
Trying last-known-good snapshot...
Restored from snapshot.
```

手动修复：
```bash
bounty doctor --repair
```

---

## 4. CLI 命令

### `bounty chat`

交互式对话模式（Bubbletea TUI）。

```bash
bounty chat                           # 新建会话
bounty chat --resume session-12345    # 恢复之前的会话
bounty chat --list                    # 列出最近会话
```

TUI 快捷键：
- `Ctrl+C` / `Ctrl+D` — 退出
- `Enter` — 发送消息
- `Backspace` — 删除字符

### `bounty run`

单次执行模式。

```bash
bounty run "列出所有TODO并添加优先级"
bounty run "修复 src/main.go 中的空指针异常"
```

### `bounty doctor`

配置诊断。

```bash
bounty doctor           # 检查配置
bounty doctor --repair  # 从快照修复配置
```

输出示例：
```
✅ Config valid
   Default model: deepseek/deepseek-v4-pro
   Providers: 2
   - deepseek (openai): DEEPSEEK_API_KEY ✅
   - anthropic (anthropic): ANTHROPIC_API_KEY ✅
   Max steps: 50
   Compact ratio: 0.8
   Builtin tools: 14 (core)
```

### `bounty serve`

启动 HTTP Gateway 服务。

```bash
bounty serve
```

Gateway 端点：

| 端点 | 方法 | 用途 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/channels` | GET | 列出活跃通道 |
| `/chat` | POST | 发送消息 `{"message":"..."}` |
| `/events` | GET | SSE 事件流（实时） |
| `/webhook/{id}` | POST | Webhook 通道接收 |

> **安全说明**：Gateway 默认仅监听 `127.0.0.1:8080`。所有端点受 `BOUNTY_AUTH_TOKEN` 保护（未设置该环境变量时放行，仅限本地开发）；请求需携带 `Authorization: Bearer <token>` 头（SSE 可用 `?token=` 查询参数）。Webhook 请求体上限 1 MiB。

### `bounty dashboard`

启动 Web 仪表盘。

```bash
bounty dashboard
# 打开 http://localhost:8080/dashboard
```

仪表盘功能：
- 会话列表
- 实时 SSE 事件流
- 工具调用可视化
- 系统状态监控

### `bounty remote`

远程 SSH 执行。

```bash
bounty remote myserver "ls -la /opt/app"
bounty remote db-server "systemctl status postgresql"
```

---

## 5. Provider 设置

### DeepSeek

```toml
[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"
context_window = 1000000
effort = "high"
```

环境变量：
```bash
export DEEPSEEK_API_KEY="sk-your-key"
```

### Anthropic (Claude)

```toml
[[providers]]
name = "anthropic"
kind = "anthropic"
models = ["claude-sonnet-5", "claude-opus-4-8", "claude-haiku-4-5"]
api_key_env = "ANTHROPIC_API_KEY"
context_window = 200000
```

支持 Thinking/Reasoning（Claude 3.5+）。当 `effort = "high"` 或 `"max"` 时自动启用。

### Ollama（本地模型）

```toml
[[providers]]
name = "ollama"
kind = "ollama"
base_url = "http://localhost:11434"
models = ["llama3.1:8b", "qwen2.5:14b", "codellama:13b"]
```

前提：Ollama 已安装并运行：
```bash
ollama serve
ollama pull llama3.1:8b
```

### OpenAI

```toml
[[providers]]
name = "openai"
kind = "openai_native"
models = ["gpt-4o", "gpt-4o-mini"]
api_key_env = "OPENAI_API_KEY"
context_window = 128000
```

### Credential Pool（多 Key 轮转）

```bash
# 多个 Key 用逗号分隔——自动轮转
export DEEPSEEK_API_KEY="key1,key2,key3"
```

行为：
- `429 Rate Limit` → 自动切到下一个 Key
- `401/403 Auth` → 标记当前 Key 耗尽，永久切换
- 所有 Key 耗尽 → 返回错误

---

## 6. 工具参考

### 文件操作

| 工具 | 只读 | 说明 |
|------|:---:|------|
| `read_file` | ✅ | 读取文件（支持 offset/limit，UTF-16 检测） |
| `write_file` | ❌ | 创建/覆盖文件（自动创建父目录） |
| `edit_file` | ❌ | 精确字符串替换（唯一性检查 + replace_all） |
| `multi_edit` | ❌ | 批量编辑同一文件 |
| `delete_range` | ❌ | 删除行范围 |
| `delete_symbol` | ❌ | 删除命名符号（tree-sitter） |

### 搜索

| 工具 | 只读 | 说明 |
|------|:---:|------|
| `grep` | ✅ | 正则搜索（优先 ripgrep，回退 Go regex） |
| `glob` | ✅ | 文件模式匹配（最多 100 结果） |
| `web_search` | ✅ | DuckDuckGo 搜索（免费，无需 API Key） |
| `web_fetch` | ✅ | HTTP GET 抓取（1MB 限制） |
| `code_index` | ✅ | 代码符号索引（Go/Python/TS/JS/Rust） |

### 执行

| 工具 | 只读 | 说明 |
|------|:---:|------|
| `bash` | ❌ | Shell 命令（120s 超时，支持 Docker 沙箱） |
| `browser` | ✅ | Chrome DevTools Protocol（start/navigate/screenshot/content/click/type） |

### 管理

| 工具 | 只读 | 说明 |
|------|:---:|------|
| `todo_write` | ✅ | 任务列表（无副作用，仅模型可见） |
| `remember` | ❌ | 保存记忆到 `.agent/memory/`（注入扫描） |
| `task` | ❌ | 派发写者子代理（上下文隔离，深度限制） |
| `read_only_task` | ✅ | 派发只读子代理（研究/探索） |
| `fleet` | ❌ | 并行派发 2-64 子代理（路径冲突检测） |

### 工具所有权

每个工具标记来源：

| Kind | 含义 |
|------|------|
| `core` | Bounty 内置 |
| `plugin` | 插件提供 |
| `channel` | 通道提供 |
| `mcp` | MCP 服务器提供 |

---

## 7. 权限系统

### 四种运行时姿态

```bash
# ask — 每个危险操作确认（默认）
# auto — 白名单放行，危险操作确认
# yolo — 全自动（Guardian 守护）
# plan — 只允许只读工具
```

在 `bounty.toml` 中配置白名单/黑名单（见[配置](#3-配置)）。

### Bash 模式匹配

白名单用前缀匹配：

```toml
bash_patterns = [
    "git status *",    # 匹配 git status、git status --short
    "go build *",      # 匹配 go build ./...、go build -o bin
    "python *",        # 匹配所有 python 命令
    "*"                 # ⚠️ 兜底：放行所有（安全依赖黑名单）
]
```

黑名单精确拒绝：

```toml
deny_bash = [
    "rm -rf *", "sudo *",
    "git push --force *",
    "shutdown *", "reboot *"
]
```

黑名单优先级 > 白名单。

### 🟢🟡🔴 三级安全

| 级别 | 工具 | 规则 |
|------|------|------|
| 🟢 | Read, Glob, Grep, TodoWrite | 自由使用 |
| 🟡 | Write, Edit, Bash(sandbox) | 有副作用但可控 |
| 🔴 | Bash(no-sandbox) | 必须确认 |

### 10 条高风险黑名单

1. 不暴露 `.env`、密钥、token
2. 不 force push（无分支保护时）
3. 不修改系统级配置
4. 不在生产库跑迁移/DDL
5. 不让同一 AI 既改文档又评文档
6. 不把整个仓库 dump 进上下文
7. 不在 hook matcher 中用 `*`
8. 不删除失败的报错/日志
9. 不把内部项目名/路径发到 WebSearch
10. 单次 fleet ≤ 50 agent

---

## 8. Hook 系统

### 9 个生命周期事件

| 事件 | 触发时机 | 决策能力 |
|------|----------|----------|
| `SessionStart` | 会话开始 | 注入上下文 |
| `UserPromptSubmit` | 用户发消息 | 验证/阻止/添加上下文 |
| `PreToolUse` | 工具执行前 | Allow/Deny/Ask/Modify |
| `PostToolUse` | 工具执行后 | 审查结果+反馈 |
| `Stop` | Agent 要停止 | **Approve/Block**（可强制继续） |
| `SubagentStop` | 子代理要停止 | Approve/Block |
| `PreCompact` | 压缩前 | 保护关键信息 |
| `Notification` | 任何通知 | 日志/反应 |
| `SessionEnd` | 会话结束 | 清理/持久化 |

### 配置 Shell Hook

```toml
[hooks]
enabled = true

[[hooks.shell]]
event = "PreToolUse"
matcher = "bash"
command = "./scripts/validate-bash.sh"
timeout_seconds = 30

[[hooks.shell]]
event = "Stop"
matcher = "*"
command = "./scripts/check-completion.sh"
timeout_seconds = 60
```

### Hook 输入/输出

Hook 通过 `HOOK_PAYLOAD` 环境变量接收 JSON：

```json
{
  "event": "PreToolUse",
  "session_id": "session-12345",
  "tool_name": "bash",
  "tool_input": {"command": "rm file.txt"}
}
```

Hook 通过 stdout JSON 返回结果：

```json
{
  "continue": true,
  "systemMessage": "Command validated"
}
```

退出码 2 = 阻止操作。

---

## 9. 技能系统

### 技能文件格式

```markdown
---
name: Code Review
description: Use when reviewing PRs or code changes
triggers: ["review", "code review", "PR"]
run_as: subagent
model: sonnet
tools: [Read, Grep, Glob]
read_only: true
---

# Code Review Skill

审查代码变更，关注：
1. 正确性——逻辑是否正确
2. 安全性——是否存在注入/越权
3. 性能——是否存在不必要的循环/内存分配
4. 代码风格——是否遵循项目规范
```

### 技能目录

```
.bounty/skills/
  code-review/          ← 子代理技能
    SKILL.md
    references/
      checklist.md
  frontend-design/      ← 内联技能
    SKILL.md
    examples/
      button.html
```

### 三级加载（缓存友好）

```
Level 1 (始终): name + description + triggers → System Prompt
Level 2 (触发时): 正文 → turn 内注入
Level 3 (按需): references/ + examples/
```

上限 4000 字符——超出部分不在 System Prompt 中展示。

### 技能生命周期（Curator）

```
active ──(30天未用)──→ inactive ──(90天)──→ archived
  ↕ pin 可跳过自动变迁
```

管理员可 Pin 技能防止自动变迁：
```toml
[skills]
disabled_skills = ["old-skill"]
```

---

## 10. 通道系统

### 内置通道

| 通道 | 类型 | 说明 |
|------|------|------|
| **Terminal** | stdin/stdout | 本地 REPL（`bounty chat`） |
| **Webhook** | HTTP | 外部服务通过 POST 发消息 |
| **HTTP API** | REST | 程序化接入（`/api/message`） |
| **Telegram** | Bot | Telegram Bot API |
| **Gateway SSE** | SSE | 实时事件流 |

### 使用 Webhook 通道

```bash
curl -X POST http://localhost:8080/webhook/myapp \
  -H "Content-Type: application/json" \
  -d '{"text": "检查服务器状态", "user_id": "admin"}'
# → 202 Accepted
```

### 使用 HTTP API 通道

```bash
# 发送消息（设置 BOUNTY_AUTH_TOKEN 后需带认证头）
curl -X POST http://127.0.0.1:9090/api/message \
  -H "Authorization: Bearer $BOUNTY_AUTH_TOKEN" \
  -d '{"text": "分析今天的日志", "user_id": "ops"}'

# 查看状态
curl http://127.0.0.1:9090/api/status
```

> **安全说明**：HTTP API 通道与 Gateway 一样默认仅监听 `127.0.0.1`，且受 `BOUNTY_AUTH_TOKEN` 保护——这些端点可以直接驱动 agent（含文件读写），切勿暴露到公网。

### Telegram Bot 配置

```toml
[telegram]
token_env = "TELEGRAM_BOT_TOKEN"
```

```bash
export TELEGRAM_BOT_TOKEN="123456:ABC-DEF1234gh"
bounty serve   # Bot 自动开始轮询
```

### 开发新通道

实现 `channel.ChannelPlugin` 接口（12 方法），注册到 Registry：

```go
type MyChannel struct{}

func (m *MyChannel) ID() string           { return "mychannel" }
func (m *MyChannel) Name() string         { return "My Channel" }
func (m *MyChannel) Start(ctx) error      { /* 启动 */ }
func (m *MyChannel) Stop(ctx) error       { /* 停止 */ }
func (m *MyChannel) OnMessage(ctx, msg) error { /* 处理消息 */ }
func (m *MyChannel) Send(ctx, reply, target) error { /* 发送回复 */ }
func (m *MyChannel) IsConnected() bool    { return true }
func (m *MyChannel) HealthCheck(ctx) error { return nil }
```

---

## 11. 子代理

### 基本用法

```
用户: "审查所有 Go 文件的安全性"
Bounty 调用 task 工具创建子代理
  → 子代理: read_file + grep → 分析 → 返回报告
Bounty: 展示子代理的报告
```

### Task（单个子代理）

```json
{
  "task": "审查 src/auth/ 目录的安全问题",
  "write_paths": []  // 只审查，不写
}
```

### Read-Only Task（只读研究）

```json
{
  "task": "研究 Go 1.25 的新特性对项目的影响"
}
// 子代理只有只读工具，无法修改文件
```

### Fleet（并行子代理，2-64 个）

```json
{
  "tasks": [
    {"task": "审查 frontend/ 代码", "write_paths": []},
    {"task": "审查 backend/ 代码", "write_paths": []},
    {"task": "审查 database/ 代码", "write_paths": []},
    {"task": "审查 tests/ 代码", "write_paths": []}
  ]
}
```

Fleet 自动检测 `write_paths` 重叠——冲突时拒绝全部任务，防止数据竞争。

### 关键约束

- 深度限制：`maxSubagentDepth = 2`（子代理不能再派孙子代理）
- 写者限制：`maxParallelWriters = 3`
- 上下文隔离：子代理不看到父对话，只返回摘要
- 工具过滤：子代理自动去掉 `task`/`fleet`/`parallel_tasks`

---

## 12. 记忆系统

### 四层记忆（启动时自动加载）

```
Level 1: 项目 BOUNTY.md            ← 最高优先级
Level 2: 项目 AGENTS.md            ← 降级方案
Level 3: ~/.config/bounty/BOUNTY.md ← 用户全局
Level 4: 祖先目录 BOUNTY.md         ← Monorepo 场景
```

### 自动记忆（remember 工具）

Agent 对话中保存记忆：

```
用户: "记住我们使用 PostgreSQL 14，连接池 20"
Bounty: 调用 remember 工具
  → 写入 .agent/memory/postgres-config.md
  → 更新 .agent/memory/MEMORY.md 索引
  → 下次会话自动加载
```

### 注入扫描

写入记忆前自动扫描 14 种 Prompt 注入模式：
- `<system>`, `<instruction>` 标签
- "ignore previous instructions"
- "pretend you are"
- "DAN mode"
- ...

检测到可疑内容 → 拒绝写入。

### 漂移检测

如果外部工具（vim/vscode）直接编辑了记忆文件，Bounty 会在下次写入时检测到冲突并警告。

---

## 13. 上下文管理

### 8 层缓存稳定性

```
1. System Prompt 构建一次，永不修改
2. Tool Schema 注册时 canonicalize + 按名称排序
3. Skills 索引仅 name+desc（上限 4000 字符）
4. 环境探针结果缓存（sync.Once）
5. 记忆更新走 turn tail，不动前缀
6. 后台任务完成通知走 turn tail
7. Hook 上下文走 turn tail
8. Plan/Goal 标记走 turn tail
```

### 3 级压缩

| 级别 | 阈值 | 行为 |
|------|:---:|------|
| Soft | 50% | 提示"上下文使用过半" |
| Compact | 80% | 摘要旧消息，保留尾部 |
| Force | 90% | 只保留 System + 最后 2 条 |

### Checkpoint/Rewind

每轮自动保存文件快照：

```
.session-checkpoints/
  turn-1.json
  turn-2.json
  turn-3.json    ← 可恢复到这一轮
```

恢复时：文件回退 + 对话截断到该轮。

---

## 14. 自改进

### Background Review（后台反思）

每 3 轮对话后，Bounty 自动 fork 自己反思：
- "这段对话中有值得保存的知识吗？"
- "有没有可提取为技能的模式？"

结果以 Notification 形式展示。

### Skill Nudge

连续 5 轮未使用技能 → 提示保存。

### Curator（技能维护）

启动时自动检查技能生命周期：
- 30 天未用 → inactive
- 90 天未用 → archived
- Pin 技能可跳过自动变迁

### Session Insights

`/save` 或 `Ctrl+C` 时输出会话统计：

```
## Session Insights
- Duration: 23m15s
- Turns: 12
- Tokens: 45K in / 8K out
- Top tools: bash(8), read_file(5), grep(3)
```

### Learning Graph

技能/工具/记忆之间的关系图。导出为 DOT 格式：

```
skill:code-review → tool:read_file (uses, weight:5.0)
skill:code-review → tool:grep (uses, weight:3.2)
memory:postgres → concept:database (related_to, weight:0.5)
```

---

## 15. MCP 集成

### 配置 MCP 服务器

```toml
[[plugins]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp@latest"]

[[plugins]]
name = "filesystem"
command = "npx"
args = ["-y", "@anthropic/mcp-filesystem", "/path/to/allowed/dir"]

[[plugins]]
name = "postgres"
command = "npx"
args = ["-y", "@anthropic/mcp-postgres", "$DATABASE_URL"]
```

### MCP 工具命名

MCP 工具自动注册为 `mcp__<server>__<tool>`：

```
mcp__playwright__browser_navigate
mcp__playwright__browser_screenshot
mcp__filesystem__read_file
mcp__postgres__query
```

---

## 16. 部署

### 单二进制

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o bounty ./cmd/bounty/
# → ~29MB 单文件
```

### Docker

```bash
docker build -t bounty-agent .
docker run -it --rm \
  -v $(pwd):/workspace \
  -e DEEPSEEK_API_KEY \
  bounty-agent chat
```

### systemd

```bash
sudo cp scripts/systemd/bounty.service /etc/systemd/system/
sudo systemctl enable --now bounty
```

### SSH 远程

```bash
bounty remote myserver "ls -la /opt/app"
bounty remote db "systemctl status postgresql"
```

---

## 17. 开发指南

### 项目结构

```
Bounty/
├── cmd/bounty/main.go        ← 入口
├── internal/
│   ├── agent/                ← Agent 引擎
│   ├── boot/                 ← 组装层
│   ├── control/              ← Controller
│   ├── tool/builtin/         ← 12 个内置工具
│   ├── provider/             ← 4 个 Provider
│   ├── channel/              ← 5 个通道
│   ├── memory/               ← 记忆系统
│   ├── store/                ← SQLite
│   ├── skill/                ← 技能系统
│   ├── hook/                 ← Hook
│   ├── permission/           ← 权限
│   ├── sandbox/              ← 沙箱
│   ├── mcp/                  ← MCP 客户端
│   ├── checkpoint/           ← 快照
│   └── ...
└── bounty.toml               ← 配置
```

### 构建命令

```bash
make build        # CGO_ENABLED=0 构建
make build-cgo    # CGO 构建（SQLite FTS5）
make test         # 运行测试
make vet          # 静态检查
make fmt          # 格式化
make docker-build # Docker 构建
```

### 添加新工具

1. 创建 `internal/tool/builtin/mytool.go`
2. 实现 `Tool` 接口（5 方法）
3. 注册到 `registry.go`

```go
type MyTool struct{}

func (MyTool) Name() string        { return "my_tool" }
func (MyTool) Description() string { return "Does something useful" }
func (MyTool) Schema() json.RawMessage { return json.RawMessage(`{...}`) }
func (MyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) { ... }
func (MyTool) ReadOnly() bool      { return true }
func (MyTool) Owner() tool.Owner   { return tool.Owner{Kind: "core", ID: "builtin"} }
```

### 添加新 Provider

1. 创建 `internal/provider/myprovider/`
2. 实现 `provider.Provider` 接口
3. 在 `boot.go` 添加 `case "myprovider":`

### 添加新技能

```bash
mkdir -p .bounty/skills/my-skill
cat > .bounty/skills/my-skill/SKILL.md << 'EOF'
---
name: My Skill
description: When to use this skill
triggers: ["keyword1", "keyword2"]
run_as: inline
---

# Skill body here
EOF
```

---

## 快速参考

| 命令 | 用途 |
|------|------|
| `bounty chat` | 交互对话 |
| `bounty run "..."` | 单次执行 |
| `bounty doctor` | 配置诊断 |
| `bounty serve` | HTTP Gateway |
| `bounty dashboard` | Web 仪表盘 |
| `bounty remote` | SSH 远程 |

| 热键 | 作用 |
|------|------|
| `Ctrl+C` | 退出 |
| `Enter` | 发送 |
| `/exit` | 退出 Terminal 通道 |
| `/save` | 手动保存会话 |

| 文件 | 位置 |
|------|------|
| 项目配置 | `./bounty.toml` |
| 用户配置 | `~/.config/bounty/config.toml` |
| 项目记忆 | `./BOUNTY.md` |
| 自动记忆 | `.agent/memory/*.md` |
| 技能 | `.bounty/skills/*/SKILL.md` |
| 命令 | `.bounty/commands/*.md` |
| 代理定义 | `.bounty/agents/*.md` |
| 会话数据 | `~/.local/share/bounty/bounty.db` |
| 配置快照 | `~/.local/share/bounty/config.toml.snapshot` |
