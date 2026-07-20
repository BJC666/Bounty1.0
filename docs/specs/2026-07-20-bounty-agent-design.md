# Bounty Agent — 通用智能体框架设计文档

**版本**: v1.1
**日期**: 2026-07-20
**状态**: 设计完成，待实现
**更新**: 融入实战配置清单中的权限模式、工具分级、高风险黑名单

## 概述

Bounty 是一个通用 AI 智能体框架，整合了 OpenClaw（多通道网关）、Hermes Agent（自进化学习）、Reasonix（缓存优先编码助手）、Claude Code（插件/Hook 架构）的最佳实践。

### 核心差异化

- **三层多语言架构**：Go 核心运行时 + TypeScript 插件层 + Python AI 微服务
- **缓存优先设计**：8 层前缀缓存稳定性，支持 DeepSeek/Anthropic 自动缓存
- **自进化闭环**：Background Review + Curator + 技能完整生命周期
- **Controller 模式**：一个后端驱动 TUI/SSE/Desktop 三个前端
- **Fleet 并行子代理**：2-64 并行 + 路径冲突检测 + 写者调度

### 参考项目

| 项目 | 借鉴的核心优点 |
|------|---------------|
| OpenClaw | 双 DB 架构、工具所有权模型、ChannelPlugin 接口、Doctor 迁移 |
| Hermes Agent | Background Review、Curator、FTS5、Credential Pool、技能安全扫描 |
| Reasonix | 缓存稳定性、Controller 模式、Fleet、Checkpoint、Safe Mode |
| Claude Code | 9 事件 Hook、5 组件插件、3 层权限、企业 MDM |

---

## 架构总览

### 三层多语言架构

```
┌──────────────────────────────────────────────────────┐
│ 第3层 · Python — AI 专业服务层 (Phase 3)              │
│ 记忆嵌入 │ 自改进 │ 轨迹分析 │ RAG 管线 │ 评估        │
│ 通过 MCP / gRPC 与核心通信                            │
├──────────────────────────────────────────────────────┤
│ 第2层 · TypeScript — 插件 & 通道层 (Phase 2)          │
│ 插件 SDK │ Channel 适配器 │ MCP 服务 │ Web UI         │
│ goja 内嵌 JS 运行时 │ npm 生态复用                    │
├──────────────────────────────────────────────────────┤
│ 第1层 · Go — 核心运行时 (Phase 1)                     │
│ Agent 循环 │ 工具注册表 │ Provider 抽象 │ 缓存引擎     │
│ 权限门控 │ 子代理调度 │ 配置管理 │ 单二进制部署        │
└──────────────────────────────────────────────────────┘
```

### 核心接口（16 个，全部在 Phase 1 定义）

```
Runner.Run(ctx, input) error           ← agent → controller
Provider.Stream(ctx, msgs, tools, opts) ← agent → provider
Tool.Execute(ctx, args) (string, error)  ← agent → tool
Registry.Schemas() []json.RawMessage     ← agent → tool (cached)
Gate.Check(ctx, tool, args) (Decision, error) ← agent → permission
Store.Save(sessionID, msgs) error       ← agent → storage
Loader.Load(root) ([]MemoryDoc, error)  ← boot → memory
SkillStore.Discover(paths) []SkillIndex ← boot → skill
Runner.Fire(event, payload) []HookResult ← agent → hook
Sink.Emit(event)                         ← agent → frontend
Controller.Send(text) error              ← frontend → controller
Compose(raw) string                      ← controller → agent input
Sandbox.Wrap(cmd) Cmd                    ← tool → sandbox
Store.Snapshot(turn, prompt, idx)        ← agent → checkpoint
Compact(session, budget) Session         ← agent → context
BackgroundReview(session)                ← turn-finalizer → fork
```

---

## 18 结构详细设计

### 结构 #1：核心 Agent 循环

**参考**: Reasonix（架构）+ Hermes（错误分类 + 预算）

**决策**: 单 Agent 单模型，后续通过 Runner 接口扩展 Coordinator/MoA

```
Agent.Run(ctx, input):
  for step := 0; step < maxSteps; step++:
    1. drain steerQueue
    2. checkStormSig + repeatSuccessCounts
    3. applyStormBreaker (if needed)
    4. provider.Stream(messages, tools) → text, toolCalls, usage
    5. tool-call repair (JSON fix + arg validation)
    6. append assistant message to session
    7. if no toolCalls → checkFinalReadiness → return
    8. for each toolCall:
       a. Gate.Check → PreToolUse hook → Execute → PostToolUse hook
       b. append tool result to session
    9. tool guardrails: loop detection / repeat failure / no progress
    10. maybeCompact()
```

**关键组件**:
- `IterationBudget`: 常规配额 + grace 配额 + 可配置上限（参考 Hermes）
- `stormSig`: 检测 (tool, error) 重复模式，触发 storm breaker
- `repeatSuccessCounts`: 检测相同写操作重复执行
- `blockedTurnStreak`: 连续无进展轮次计数
- `finalReadinessGate`: 确认 todo_write + 验证步骤完成（Delivery 模式）
- `tool-call repair`: JSON 修复 + 参数验证（参考 Reasonix repair 包）

**Go Package**: `internal/agent/` — agent.go, session.go, compact.go, task.go, fleet.go

---

### 结构 #2：工具系统

**参考**: Reasonix（接口 + Registry）+ OpenClaw（所有权模型）+ Hermes（并行执行器）

**核心接口**:
```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage) (string, error)
    ReadOnly() bool
}

// 可选能力（类型断言）
type Previewer interface { Preview(args) → change }
type SnipHinter interface { SnipHint(output) → hint }
type OwnerRef interface { Owner() → {kind, id} }
```

**工具所有权**:
```
core → 内置工具
plugin → 插件工具（Phase 2）
mcp → MCP 服务器工具（Phase 2/3）
```

**执行模式**: 纯只读工具并行（ThreadPool, max 8），涉及写操作顺序执行

**输出预算**: 单工具 32KB / 单轮按上下文窗口动态缩放 / bash 超时 120s（可配置）

**Phase 1 内置工具（9 个）**:

| 工具 | 类型 | 参考 |
|------|------|------|
| bash | 读写 | Reasonix bash.go（前台+后台+超时+取消） |
| read_file | 只读 | Reasonix read_file.go（off/limit + UTF-16） |
| write_file | 读写 | Reasonix write_file.go |
| edit_file | 读写 | Reasonix edit_file.go（精确字符串替换） |
| grep | 只读 | Reasonix grep.go（ripgrep 原生） |
| glob | 只读 | Reasonix glob.go |
| web_fetch | 只读 | Reasonix web_fetch.go |
| web_search | 只读 | Hermes web_tools.py |
| todo_write | 只读 | Reasonix todo_write.go（无副作用） |

**Go Package**: `internal/tool/` — tool.go, registry.go, contract.go
**Go Package**: `internal/tool/builtin/` — 每个工具一个文件 + init() 注册

---

### 结构 #3：模型/Provider 系统

**参考**: Hermes（Provider 矩阵 + Credential Pool）+ Reasonix（缓存友好）

**核心接口**:
```go
type Provider interface {
    Stream(ctx, messages, tools, opts) → Stream
}

type Stream interface {
    Next() → {Delta, ToolCall, Usage, Done, Err}
}

func CanonicalizeSchema(raw json.RawMessage) json.RawMessage
```

**Phase 1 Provider**:
1. **DeepSeek** (OpenAI 兼容协议) — 1M 上下文，去掉 reasoning_content 重传
2. **Anthropic** (Messages API 原生) — 保留 signed reasoning block

**错误分类器（8 类，参考 Hermes error_classifier.py）**:
| 类别 | 识别 | 恢复 |
|------|------|------|
| RateLimit | 429 + Retry-After | 指数退避 + jitter，最多 5 次 |
| ContextOverflow | 400 + context_length_exceeded | 触发压缩 → 重试 1 次 |
| AuthError | 401/403 | 切换 credential pool 下一个 key |
| ServerError | 5xx | 线性退避，最多 3 次 |
| ContentFilter | 400 + content_filter | 裁剪最近消息 → 重试 1 次 |
| OutputCap | 400 + max_tokens | 降低 max_tokens → 重试 |
| NetworkError | timeout/DNS/reset | 指数退避，最多 3 次 |
| FatalError | 其他 | 立即返回，不重试 |

**Credential Pool**（参考 Hermes credential_pool.py）:
- 多 Key 轮转: 429 → 切换，401 → 标记耗尽
- api_key_env = "KEY1,KEY2" → 自动轮转

**Go Package**: `internal/provider/` — provider.go, canonicalize.go
**Go Package**: `internal/provider/openai/` — (DeepSeek 复用)
**Go Package**: `internal/provider/anthropic/` — 原生实现

---

### 结构 #4：记忆/持久化系统

**参考**: OpenClaw（双 DB）+ Hermes（FTS5 + 记忆文件）+ Reasonix（层次化加载）

**三层记忆架构**:

```
第3层 — 会话状态 (SQLite)
  WAL 模式 │ sessions + messages 表 │ FTS5 全文索引
  参考: Hermes hermes_state.py

第2层 — 项目记忆 (Markdown 文件)
  REASONIX.md > AGENTS.md > ~/.config/agent/REASONIX.md > 祖先目录
  参考: Reasonix memory 包

第1层 — 自动记忆 (Agent 写入)
  .agent/memory/*.md (frontmatter 索引) → MEMORY.md 总索引
  参考: Reasonix auto-memory store
```

**SQLite Schema**:
```sql
sessions(id, title, model, provider, source, cwd, system_prompt, parent_id, created_at, updated_at)
messages(id, session_id, role, content, tool_calls, tool_name, usage, created_at)
messages_fts USING fts5(content, content_rowid=id, content='messages')
```

**记忆文件层次**（启动时合并，项目 > 用户 > 全局 > 祖先，后者不覆盖前者）

**记忆漂移检测**（参考 Hermes）: 检测外部工具编辑 → 拒绝冲突写入

**记忆注入扫描**（参考 Hermes）: 写入前检测 prompt 注入模式 → 拒绝可疑内容

**Honcho 用户建模**（Phase 3 MCP 实现，参考 Hermes + Plastic Labs）

**Go Package**: `internal/store/` — sqlite.go, schema.sql
**Go Package**: `internal/memory/` — loader.go, remember.go, drift.go
**Go Package**: `internal/history/` — session.go, search.go

---

### 结构 #5：插件/扩展系统

**参考**: Claude Code（5 组件类型，最清晰）

**5 种组件**:
| 组件 | 位置 | 格式 | Phase |
|------|------|------|-------|
| Commands | `commands/*.md` | Markdown + YAML frontmatter | 1 |
| Skills | `skills/<name>/SKILL.md` | Markdown + YAML frontmatter | 1 |
| Agents | `agents/*.md` | Markdown + YAML frontmatter | 子代理实现后 |
| Hooks | `hooks/hooks.toml` | TOML | 1 |
| MCP Servers | `.mcp.json` | JSON | 2 |

**插件清单**: `plugin.toml` — name, version, description, permissions

**发现顺序**（后者覆盖）: 内置 → 项目(.agent/plugins/) → 用户(~/.config/agent/plugins/) → MCP

**权限声明**: `permissions = ["read:files", "execute:bash"]`

**Go Package**: `internal/plugin/` — manifest.go, discovery.go, lifecycle.go
**Phase 2**: TypeScript SDK + goja 内嵌运行时

---

### 结构 #6：通道/消息系统

**参考**: OpenClaw（30+ slot ChannelPlugin 接口）

**Phase 2 实现**。核心抽象:

```go
type ChannelPlugin interface {
    // 30+ adapter slots from OpenClaw
    Config, Setup, Pairing, Security, Groups, Mentions
    Outbound, Status, Gateway, Auth, Commands, Lifecycle
    Secrets, Bindings, Streaming, Threading, Message
    AgentPrompt, Directory, Resolver, Actions, Heartbeat
}
```

**设计原则**: 通道纯传输 — 渲染可移植展示/动作，不拥有产品逻辑

**Go Package**: `internal/channel/` (Phase 2)

---

### 结构 #7：技能系统

**参考**: Hermes（完整生命周期）+ Reasonix（缓存友好索引）+ Claude Code（trigger 机制）

**SKILL.md 格式**:
```yaml
---
name: Frontend Design
description: Use when building UI components...
triggers: ["frontend", "UI design", "styling"]
run_as: subagent        # inline | subagent
model: sonnet           # 覆盖默认模型
tools: [Read, Grep, Write]
read_only: false
---
# 技能正文（只在触发时加载）
```

**三级加载**:
- Level 1 (always): name + description + triggers → 进入 System Prompt 索引（上限 4000 字符）
- Level 2 (on-trigger): 正文 → turn 内注入
- Level 3 (on-demand): references/ + examples/ → 按需加载

**技能生命周期**（参考 Hermes）:
```
active (30天未用) → inactive (90天) → archived
  ↕ pin 可跳过自动变迁
  ↕ Curator 可合并相关技能
```

**技能安全扫描**（参考 Hermes skills_guard.py）:
- 安装时 AST 审计: 检测 import/exec/eval/shell/network/filesystem
- Trust signals: 来源仓库 + commit + 作者
- Quarantine: 可疑技能隔离，不加载

**Phase 1 内置技能**:
| 技能 | run_as | 来源 |
|------|--------|------|
| code-review | subagent | Reasonix review |
| explore | subagent | Reasonix explore |
| research | subagent | Reasonix research |
| security-review | subagent | Reasonix security_review |
| frontend-design | inline | Claude Code |

**Go Package**: `internal/skill/` — skill.go, index.go, store.go, tools.go, guard.go

---

### 结构 #8：Hook/生命周期系统

**参考**: Claude Code（9 事件 + 双模式）

**9 个事件**:
| 事件 | 时机 | 决策能力 |
|------|------|----------|
| SessionStart | 会话开始 | 注入上下文 |
| UserPromptSubmit | 用户发消息 | 添加上下文/验证/阻止 |
| PreToolUse | 工具执行前 | Allow / Deny / Ask / Modify |
| PostToolUse | 工具执行后 | 审查结果 + 反馈 |
| Stop | Agent 要停止 | Approve / Block |
| SubagentStop | 子代理要停止 | Approve / Block |
| PreCompact | 压缩前 | 保护关键信息 |
| Notification | 任何通知 | 日志/反应 |
| SessionEnd | 会话结束 | 清理/持久化 |

**Hook 类型**: Shell 命令模式（Phase 1），Prompt-based 模式（Phase 2）

**Hook 匹配器**: 正则匹配工具名称，`*` 通配

**Go Package**: `internal/hook/` — runner.go, events.go, shell.go

---

### 结构 #9：安全/权限系统

**参考**: Claude Code（3 层设置）+ Reasonix（Gate 接口 + OS 沙箱）+ 实战配置清单（模式匹配 + 三级工具安全）

**Gate 接口**:
```go
type Gate interface {
    Check(ctx, tool, args) (Decision, error)
}
type Decision int  // Allow | Deny | Ask
```

**4 种运行时姿态**:
- `ask` — 每个危险操作确认
- `auto` — 白名单放行，危险操作确认
- `yolo` — 全自动（Guardian 守护）
- `plan` — 只允许只读工具

**权限规则 — 必须严格遵守以下白名单/黑名单**:

```toml
[permissions.allow]
# ── 只读工具：无条件放行 ──
tools = [
    "Read", "Glob", "Grep", "WebSearch", "WebFetch",
    "Skill", "TodoWrite", "TaskOutput", "Agent", "Workflow",
    "AskUserQuestion", "EnterPlanMode", "ExitPlanMode",
    "CronCreate", "CronDelete", "CronList", "ScheduleWakeup",
    "EnterWorktree", "ExitWorktree",
    "Edit", "Write", "NotebookEdit"
]

# ── Bash 白名单（模式匹配，按分类） ──
bash_allow = [
    # 无害命令
    "ls *", "cat *", "head *", "wc *", "pwd", "which *", "echo *", "cd *", "find *",
    # Git 只读
    "git status *", "git diff *", "git log *", "git branch *", "git remote *",
    # Git 写入
    "git add *", "git commit *", "git checkout *", "git switch *",
    "git merge *", "git pull *", "git push *", "git stash *", "git fetch *",
    # 包管理
    "npm run *", "npm install *", "npm exec *", "npm test *",
    "pip install *", "pip list *", "pip show *",
    # 运行
    "python *", "flask *", "pytest *", "npx *",
    # 文件操作
    "mkdir *", "touch *", "cp *", "mv *", "curl *",
    # 兜底（⚠️ 已知风险：几乎放行全部 bash，安全依赖黑名单兜底）
    "*"
]

[permissions.deny]
# ── Bash 黑名单 ──
bash_deny = [
    "rm -rf *",           # 递归强制删除
    "rm *",               # 普通删除
    "sudo *",             # 提权操作
    "git push --force *", # 强制推送
    "git reset --hard *", # 硬重置
    "git clean *",        # Git 清理
    "chmod 777 *",        # 危险权限
    "docker rm *",        # 删除容器
    "docker rmi *",       # 删除镜像
    "shutdown *",         # 关机
    "reboot *",           # 重启
    "format *",           # 格式化磁盘
]

# ── 文件系统保护 ──
forbid_write = [
    "Windows/*",          # Windows 系统目录
    "Program Files/*",    # 程序安装目录
    "Program Files (x86)/*",
    "System32/*",
    "/etc/*",             # Linux 系统配置
    "/boot/*",
    "~/.ssh/*",           # SSH 密钥
]
```

**🟢🟡🔴 三级工具安全**（参考实战配置清单）:
| 级别 | 工具 | 规则 |
|------|------|------|
| 🟢 自由使用 | Read, Glob, Grep, TodoWrite, AskUserQuestion | 只读/无副作用 |
| 🟡 需谨慎 | Write, Edit, Bash(sandbox), WebSearch, WebFetch | 有副作用但可控 |
| 🔴 必须确认 | Bash(no-sandbox), CronCreate(durable), Workflow(agent>10) | 显式用户确认 |

**高风险行动黑名单（10 条，参考实战配置清单）**:
1. 不暴露 .env、密钥、token
2. 不 force push（无分支保护时）
3. 不修改系统级配置
4. 不在生产库跑迁移/DDL
5. 不让同一 AI 既改文档又评文档
6. 不把整个仓库 dump 进上下文
7. 不在 hook matcher 中用 `*`
8. 不删除失败的报错/日志
9. 不把内部项目名/路径/架构发到 WebSearch
10. 单次 Workflow ≤ 50 agent；token 密集型操作先告知

**3 层配置优先级**: Enterprise > User (~/.config/agent/) > Project (.agent/)

**OS 沙箱**（参考 Reasonix sandbox 包）:
- 文件系统: workspace_root 可写 + allow_write 白名单 + forbid_read 黑名单
- 网络: allowed_domains + allow_local_binding + HTTP/SOCKS 代理

**Guardian 审查**（参考 Reasonix guardian 包）:
- yolo 模式下的二次审查守护
- 自动审批窗口 + 敏感操作升级

**Safe Mode 启动**（参考 Reasonix）:
- 检测上次异常退出 → 跳过 Plugin/MCP/迁移/cleanup
- 防止损坏状态阻塞启动

**Go Package**: `internal/permission/` — gate.go, policy.go, posture.go, patterns.go
**Go Package**: `internal/sandbox/` — confine.go, network.go
**Go Package**: `internal/guardian/` — guardian.go

---

### 结构 #10：子代理/委托系统

**参考**: Reasonix（最干净的实现）

**子代理工具**:
| 工具 | 能力 | 并行度 |
|------|------|--------|
| task | 单个写者子代理 | 1 |
| read_only_task | 单个只读子代理 | 1 |
| fleet | 写者并行 | 2-64 |
| parallel_tasks | 只读并行 | N |

**关键约束**:
- maxSubagentDepth = 2（防止无限递归）
- maxParallelWriters = 3（防止磁盘抖动）
- fleet preflight: 写路径重叠 → 全部拒绝
- contextIsolation: 子代理不看父上下文，只返回摘要
- toolFiltering: 子代理自动去掉 task/fleet/parallel/jobs

**Go Package**: `internal/agent/task.go` + `fleet.go` + `parallel_tasks.go` + `scheduler.go`

---

### 结构 #11：配置系统

**参考**: Reasonix（TOML + 快照 + 自动修复）

**配置格式**: TOML

**3 级解析**:
1. 命令行 flag（最高优先）
2. `./agent.toml`（项目）
3. `~/.config/agent/config.toml`（用户）
4. 内置默认（最低优先）

**关键配置项**:
```toml
config_version = 1
default_model = "deepseek/deepseek-v4-pro"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"
context_window = 1_000_000

[agent]
temperature = 0.0
compact_ratio = 0.8
max_subagent_depth = 2
max_subagent_concurrency = 6
max_parallel_writers = 3

[sandbox]
workspace_root = ""
allow_write = ["/tmp"]
forbid_read = ["${HOME}/.ssh"]

[[plugins]]
name = "example"
command = "my-mcp-server"
```

**自动修复**: 损坏的 TOML → 回退到最后已知良好快照

**Secret 保护**: API Key 只在 env var，永不写入配置文件

**Go Package**: `internal/config/` — config.go, loader.go, defaults.go
**Go Package**: `internal/secrets/` — secrets.go
**Go Package**: `internal/repair/` — snapshot.go, config.go

---

### 结构 #12：CLI/UI 系统

**参考**: Reasonix Controller 模式

**一个 Controller，三个前端**:
```
Controller ← TUI (Bubbletea, Go)
           ← HTTP/SSE (嵌入 SPA)
           ← Desktop (Wails/Tauri, Go+Web)
```

**Controller 命令**（所有前端通用）:
Send(text) | Cancel() | Approve(callID) | SetPlanMode(on) | Compact() | NewSession()

**Controller 事件流**（Sink 接口）:
ReasoningDelta → TextDelta → ToolDispatch → ToolResult → Usage → TurnComplete

**Go Package**: `internal/control/` — controller.go, turn_orchestrator.go, compose.go, approval.go
**Go Package**: `internal/cli/` — Bubbletea TUI
**Go Package**: `internal/serve/` — HTTP/SSE + 嵌入 SPA
**Go Package**: `cmd/bounty/` — main.go

---

### 结构 #13：自改进/学习系统

**参考**: Hermes（唯一完整实现）

**完整闭环组件**:
| 组件 | 职责 | 触发时机 |
|------|------|----------|
| Background Review | Fork 自己 → 反思保存技能/记忆 | 每轮后 daemon 线程 |
| Skill Nudge | N 轮没用技能 → 提醒保存 | 每 N 轮检查 |
| Curator | active→inactive→archived + LLM 合并 | 每 7 天 |
| Learning Graph | 技能+记忆关系可视化 | 按需 |
| Session Insights | Token 用量 + 工具模式 + 费用趋势 | 每会话 |

**Phase 1**: 手动 /remember + 静态技能，无自动变迁
**Phase 3**: Python MCP 微服务完整实现

**Go Package**: `internal/agent/background_review.go` + `internal/agent/curator.go` (Phase 1 简单版)

---

### 结构 #14：上下文管理系统

**参考**: Reasonix（8 层缓存）+ Hermes（3 级压缩）

**8 层缓存稳定性**（全部在 Phase 1 实现）:
1. System Prompt 构建一次，永不修改
2. Tool Schema 注册时 canonicalize + 排序
3. Skills 索引仅 name+desc（上限 4000 字符）
4. 环境探针结果缓存到磁盘
5. 记忆更新走 turn tail，不动前缀
6. 后台任务完成通知走 turn tail
7. Hook 上下文走 turn tail
8. Plan/Goal 标记走 turn tail

**3 级压缩**:
- soft_compact (0.5): 通知用户上下文过半
- compact (0.8): 旧消息摘要 + 尾消息保留
- force_compact (0.9): 保留最后 2 条消息，其余全部摘要

**压缩算法**:
```
compact(session, budget):
  tail ← 最近 tokenBudget 的消息
  middle ← auxiliary model 摘要
  session.Messages ← [system] + [summary] + tail
```

**Checkpoint/Rewind 系统**（参考 Reasonix）:
- Git-free: `session.ckpt/turn-<N>.json`
- 每 turn 记录 touched files 的 pre-edit 内容
- MsgIndex 用于 conversation-rewind 边界
- 跨重启持久化

**崩溃恢复元数据**（参考 Hermes + Reasonix）:
- 关键操作前持久化 crash resilience metadata
- checkpoint 恢复最后文件状态

**Go Package**: `internal/agent/compact.go` + `internal/checkpoint/` + `internal/environment/`

---

### 结构 #15：部署/基础设施

**参考**: OpenClaw（最完整）

**部署路径**:
| 方式 | 目标 |
|------|------|
| 单二进制 | `go build -o bounty cmd/bounty/main.go` |
| Docker | 多阶段构建，alpine 基础镜像 |
| Homebrew | macOS `brew install bounty` |
| npm | 下载 Go 二进制（参考 Reasonix npm 包） |
| systemd | Linux 守护进程 |
| SSH 远程 | 参考 Reasonix remote 包 |

---

### 结构 #16：开发/调试工具

**参考**: Hermes（doctor + backup + export）+ Reasonix（缓存影响检查 + 提示词稳定性测试）

**调试工具箱**:
- `bounty doctor` — 配置诊断 + Provider 连通性 + 文件权限
- `bounty export` — 会话导出为 HTML/JSON/Markdown
- `bounty backup` — SQLite dump + tar
- 缓存影响检查: 检测前缀变化来源
- 提示词稳定性测试: CI 中自动验证
- E2E 基准: 自动化回归测试

**LSP 集成**（参考 Reasonix lsp 包）:
- gopls 客户端 → 代码索引/跳转/诊断

**Semantic Router**（参考 Reasonix capability 包）:
- Economy/Delivery profiles → 按需加载可选工具

---

### 结构 #17：API/协议支持

**参考**: OpenClaw + Reasonix（MCP 完整实现）

**MCP 完整实现**:
- Transport: stdio / HTTP+SSE / Streamable HTTP
- Tool discovery → 注册到 Registry → mcp__<server>__<tool> 命名空间
- Security fingerprint + cache validation

**ACP 协议**: Agent Client Protocol — 会话 CRUD + 流式 + 工具调用

**REST + SSE + WebSocket**: HTTP API + SSE 推送 + WS 实时双向

---

### 结构 #18：综合集成

**最终交付物**:
- CLI 二进制: `bounty chat` / `bounty run` / `bounty serve` / `bounty doctor`
- Go 核心库: `internal/*` 可作为 library 引用
- 插件 SDK: TypeScript 类型 + goja 绑定（Phase 2）
- 内置技能: 5 个
- 文档: README + CONTRIBUTING + API 参考
- 示例: 配置 + 插件 + Docker Compose
- CI/CD: lint → test → build → release

---

## 实现阶段

### Phase 1 — 纯 Go 核心（当前）

覆盖结构: #1 #2 #3 #4 #5(simple) #7(simple) #8 #9 #10 #11 #12 #14 #16(simple) #18

产出: 单二进制 CLI Agent，能对话、执行工具、记住上下文

### Phase 2 — Go + TypeScript

覆盖结构: #5(full) #6 #7(full) #12(Web UI) #17

产出: 插件生态 + 多通道支持 + Web 仪表盘

### Phase 3 — Go + TypeScript + Python

覆盖结构: #13(full) #4(Honcho) #16(LSP)

产出: 自改进闭环 + 高级记忆 + 代码智能

---

## 项目结构

```
Bounty/
├── cmd/bounty/           # 主入口
├── internal/
│   ├── agent/            # Agent 循环 + 子代理 + 压缩
│   ├── boot/             # 组装 Controller
│   ├── checkpoint/       # 快照/回退
│   ├── cli/              # Bubbletea TUI
│   ├── config/           # TOML 配置
│   ├── control/          # Controller + Turn 编排
│   ├── environment/      # 环境探针
│   ├── event/            # 事件类型 + Sink
│   ├── guardian/         # 自动审批守护
│   ├── history/          # 会话持久化
│   ├── hook/             # Hook 系统
│   ├── memory/           # 记忆加载 + 自动记忆
│   ├── permission/       # Gate + Policy
│   ├── plugin/           # 插件发现 + 加载
│   ├── provider/         # Provider 接口
│   ├── provider/openai/  # DeepSeek 适配
│   ├── provider/anthropic/ # Anthropic 适配
│   ├── repair/           # 配置修复 + 快照
│   ├── sandbox/          # OS 沙箱
│   ├── secrets/          # Secret 保护
│   ├── serve/            # HTTP/SSE 服务
│   ├── skill/            # 技能系统
│   ├── store/            # SQLite 存储
│   └── tool/             # 工具接口 + Registry
│       └── builtin/      # 内置工具
├── .agent/
│   ├── skills/           # 项目技能
│   └── memory/           # 自动记忆
├── docs/specs/           # 设计文档
├── agent.toml            # 项目配置
└── README.md
```

---

## 有意不纳入的特性（及原因）

| 特性 | 来源 | 原因 | 后续计划 |
|------|------|------|----------|
| MoA 多模型讨论 | Hermes | 成本 ×N，不适合 V1 | Runner 接口可扩展 |
| Coordinator 双模型 | Reasonix | 单模型起步，复杂度更低 | Runner 接口可扩展 |
| 6 种终端后端 | Hermes | Docker/SSH 够用 | 按需添加 |
| Kanban 多 Agent 协调 | Hermes | Fleet 已覆盖并行 | 独立插件 |
| IM Bot (飞书/QQ/微信) | Reasonix | Phase 2 通道系统统一做 | #6 通道系统 |
| Petdex 精灵宠物 | Hermes | 纯装饰 | 永不 |
