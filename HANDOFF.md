# Bounty 1.0 — 项目交接文档

> 生成日期：2026-07-31 · 版本：1.0 · 作者：Claude + 用户协作

---

## 一、项目概述

**Bounty** 是一个 Go 语言编写的通用 AI 智能体框架，系统性融合了 Claude Code（插件/Hook）、Hermes（自进化）、OpenClaw（通道）、Reasonix（缓存）四大参考项目的架构精华，并深度集成了 DeVET 多智能体委托验证系统。

**一句话**：单二进制 AI 智能体，20 个工具，5 个通道，自带 Web/TUI 双界面，支持多智能体安全验证。

### 关键数据

| 指标 | 数值 |
|------|:---:|
| Go 文件 | ~75 |
| 总文件 | ~100 |
| 总 commits | 37 |
| 包数 | 28 |
| 测试数 | 32 (全部通过) |
| 代码审查 | 3 轮，128 bugs 发现 → 71 已修复 |
| 构建产物 | ~29MB 单二进制 (CGO_ENABLED=0) |

---

## 二、项目结构

```
Bounty1.0/
├── bounty.exe                  ← 编译产物（可分发）
├── bounty.toml                 ← 项目配置
├── start.bat                   ← TUI 一键启动
├── 启动网页.bat                ← Web 控制台一键启动
├── HANDOFF.md                  ← 本文件
├── README.md                   ← 项目说明
├── GUIDE.md                    ← 使用指南
├── BOUNTY.md                   ← 项目记忆
├── Dockerfile                  ← Docker 构建
├── Makefile                    ← 构建命令
│
├── cmd/bounty/main.go          ← CLI 入口 (chat/run/serve/doctor/dashboard/remote)
│
├── internal/
│   ├── agent/                  ← Agent 引擎
│   │   ├── agent.go            ← 核心循环（流式/工具执行/重试/护栏）
│   │   ├── session.go          ← 会话管理（缓存友好）
│   │   ├── compact.go          ← 3 级 Token 压缩
│   │   ├── guardrails.go       ← 风暴检测/重复失败
│   │   ├── task.go             ← 子代理工具 (task/read_only_task)
│   │   ├── fleet.go            ← 并行子代理 (2-64, 路径冲突检测)
│   │   ├── background_review.go ← 后台反思
│   │   ├── insights.go         ← 会话分析
│   │   └── learning_graph.go   ← 技能/工具关系图
│   │
│   ├── boot/boot.go            ← 组装层（配置→Controller）
│   ├── control/controller.go   ← 传输无关会话驱动器
│   ├── event/event.go          ← 事件类型 + Sink 接口
│   │
│   ├── tool/
│   │   ├── tool.go             ← Tool 接口 + Registry + 所有权模型
│   │   └── builtin/            ← 12 个内置工具 + 5 个 DeVET 工具
│   │       ├── bash.go         ← Shell 执行（Win/Linux 自适应）
│   │       ├── read_file.go    ← 文件读取
│   │       ├── write_file.go   ← 文件写入
│   │       ├── edit_file.go    ← 精确字符串替换
│   │       ├── grep.go         ← 正则搜索（rg 优先，Go 回退）
│   │       ├── glob.go         ← 文件查找
│   │       ├── web_search.go   ← DuckDuckGo 搜索
│   │       ├── web_fetch.go    ← HTTP 抓取
│   │       ├── todo_write.go   ← 任务列表
│   │       ├── code_index.go   ← 代码符号索引
│   │       ├── browser.go      ← Chrome CDP 浏览器
│   │       ├── remember.go     ← 自动记忆
│   │       ├── devet.go        ← DeVET 5 工具
│   │       └── registry.go     ← 工具注册
│   │
│   ├── provider/               ← LLM Provider
│   │   ├── provider.go         ← Provider 接口
│   │   ├── canonicalize.go     ← Schema 规范化
│   │   ├── cache_shape.go      ← 缓存诊断
│   │   ├── openai/             ← DeepSeek/OpenAI 适配
│   │   ├── anthropic/          ← Claude 适配
│   │   ├── ollama/             ← 本地 Ollama
│   │   └── openai_native/      ← OpenAI 原生
│   │
│   ├── channel/                ← 消息通道
│   │   ├── plugin.go           ← ChannelPlugin 接口 (12 方法)
│   │   ├── gateway.go          ← HTTP Gateway
│   │   ├── webhook/            ← Webhook 通道
│   │   ├── terminal/           ← Terminal REPL 通道
│   │   ├── httpapi/            ← HTTP API 通道
│   │   └── telegram/           ← Telegram Bot 通道
│   │
│   ├── memory/                 ← 记忆系统
│   │   ├── loader.go           ← 4 级层次加载
│   │   ├── remember.go         ← 自动记忆存储
│   │   ├── drift.go            ← 外部编辑检测
│   │   └── injection.go        ← Prompt 注入扫描
│   │
│   ├── store/sqlite.go         ← SQLite + FTS5 持久化
│   ├── config/                 ← TOML 配置系统
│   ├── secrets/secrets.go      ← Credential Pool
│   ├── hook/                   ← 9 事件 Hook 系统
│   ├── permission/gate.go      ← 权限 Gate + Bash 模式匹配
│   ├── sandbox/                ← OS/Docker 沙箱
│   ├── guardian/guardian.go    ← YOLO 守护
│   ├── skill/                  ← 技能系统 + Curator
│   ├── plugin/                 ← 插件/命令/Agent 定义
│   ├── mcp/client.go           ← MCP 客户端 (JSON-RPC)
│   ├── checkpoint/             ← Git-free 快照
│   ├── devet/                  ← DeVET 集成
│   │   ├── launcher.go         ← 自动启动后端
│   │   └── types.go            ← 结构化类型
│   ├── environment/            ← 环境探针
│   ├── repair/snapshot.go      ← 配置快照修复
│   ├── remote/ssh.go           ← SSH 远程
│   ├── log/logger.go           ← 结构化日志
│   ├── cli/tui.go              ← Hermes 风格 TUI
│   └── serve/                  ← Web 服务
│       ├── chat.go             ← 聊天 SPA（含 DeVET 面板）
│       ├── dashboard.go        ← 仪表盘
│       └── export.go           ← 会话导出 (MD/HTML/JSON)
│
├── competition/                ← 竞赛文档
│   ├── 01-产品概述与定位.md
│   ├── 02-技术架构与创新.md
│   ├── 03-功能设计与实现.md
│   ├── 04-开发测试与部署.md
│   ├── 05-应用价值与展望.md
│   ├── 06-附录-代码与测试数据.md
│   ├── 演示脚本.md
│   ├── 测试报告.md
│   └── docs/specs/             ← 设计文档
│
├── docs/
│   ├── comparison.md           ← 五项目对比
│   └── superpowers/plans/      ← 实现计划
│
├── npm/                        ← npm 分发
├── scripts/homebrew/           ← Homebrew 公式
├── scripts/systemd/            ← systemd 服务
└── desktop/main.go             ← Wails 桌面（实验性）
```

---

## 三、快速启动

### 前提

1. Windows 10/11（Linux/macOS 也可，部分路径需调整）
2. DeepSeek API Key（或 Anthropic/Ollama）
3. Python 3.10+（仅 DeVET 需要）

### 一键启动

```powershell
# Web 控制台（推荐，有聊天 + DeVET 面板）
双击 启动网页.bat

# TUI 终端
双击 start.bat
```

### 手动启动

```powershell
# 设置 API Key（只需一次，新窗口自动读取注册表）
setx DEEPSEEK_API_KEY "sk-your-key"

# Web 控制台
.\bounty.exe dashboard
# 浏览器打开 http://localhost:8090

# TUI
.\bounty.exe chat

# 单次执行
.\bounty.exe run "你的问题"

# 诊断
.\bounty.exe doctor
```

### 构建

```bash
# 普通构建
go build -o bounty.exe ./cmd/bounty/

# 纯静态（无 CGO，跨平台）
CGO_ENABLED=0 go build -ldflags="-s -w" -o bounty ./cmd/bounty/

# 测试
go test ./internal/tool/ ./internal/config/ ./internal/event/ ./internal/plugin/ ./internal/skill/ -v
```

---

## 四、核心功能速览

### CLI 命令

| 命令 | 用途 |
|------|------|
| `bounty chat` | TUI 交互对话（Hermes 风格，带滚动条） |
| `bounty run "..."` | 单次执行 |
| `bounty doctor` | 配置诊断 |
| `bounty serve` | HTTP Gateway |
| `bounty dashboard` | Web 控制台 (:8090) |
| `bounty remote <host> <cmd>` | SSH 远程执行 |

### TUI 会话命令

| 命令 | 作用 |
|------|------|
| `/new 标题` | 创建新会话 |
| `/switch <id>` | 切换会话 |
| `/list` | 列出所有会话 |
| `/rename 标题` | 重命名当前会话 |

### 内置工具 (20 个：12 内置 + 5 DeVET + 3 子代理)

**文件操作**: read_file, write_file, edit_file, grep, glob
**执行**: bash (Win/Linux自适应), browser (Chrome CDP)
**搜索**: web_search (DDG), web_fetch, code_index
**管理**: todo_write, remember
**子代理**: task, read_only_task, fleet (2-64并行)
**DeVET**: devet_health, devet_build_scenario, devet_verify_chain, devet_list_attacks, devet_simulate_attack

### Provider (4 个)

DeepSeek · Anthropic (Claude) · Ollama (本地) · OpenAI

### 通道 (5 个)

Terminal REPL · Webhook · HTTP API · Telegram Bot · Gateway SSE

---

## 五、关键设计决策

### 1. Go 单二进制
选择 Go 作为核心语言的最大原因：编译成单文件，无运行时依赖。用户可以复制 `bounty.exe` 到任何 Windows 机器直接运行。

### 2. Controller 模式
所有前端（TUI/Web/Desktop）共享同一个 `control.Controller`，通过统一的 `Send()`/`Sink` 接口交互。添加新前端不需要修改核心代码。

### 3. DeVET HTTP 桥接
不修改 DeVET 任何代码。Bounty 通过 HTTP 调用 DeVET 的 REST API。Bounty 自动检测并启动 DeVET Python 后端。

### 4. 8 层缓存稳定性
针对 DeepSeek 的 prefix-cache 设计的核心优化：System Prompt 构建后永不修改，动态内容走 turn tail 注入。

### 5. 四级权限模型
ask → auto → yolo → plan，配合 Bash 模式匹配白/黑名单 + 文件系统保护。

---

## 六、已知问题与限制

### 功能限制

| 问题 | 影响 | 解决方向 |
|------|------|----------|
| DeepSeek 调用子代理时有 JSON 序列化问题 | read_only_task 偶尔失败 | 排查 content 字段 omitempty |
| 模型有时忽略 DeVET 工具用 web_search | 浪费 token | 优化 System Prompt |
| FTS5 需要 CGO | 纯静态构建无全文搜索 | LIKE 回退可用 |
| Browser 工具需要 Chrome | CDP 浏览器功能不可用 | 检测 chrome 路径 |
| Docker 沙箱需 Docker Desktop | bash 在 Docker 外执行 | `docker.Available()` 检查 |
| 无 Web 认证（默认） | 局域网可访问 | 设置 `BOUNTY_AUTH_TOKEN` |

### 性能特征

- 单次 LLM 调用延迟：取决于 API 和网络（~1-5s）
- DeVET 验证延迟：0.024ms（纯 Python 计算）
- 会话恢复：取决于消息数量（~10ms/100条）

---

## 七、竞赛提交清单

| 项目 | 状态 | 位置 |
|------|:---:|------|
| 可运行软件 | ✅ | `bounty.exe` |
| 产品概述 | ✅ | `competition/01-产品概述与定位.md` |
| 技术架构 | ✅ | `competition/02-技术架构与创新.md` |
| 功能设计 | ✅ | `competition/03-功能设计与实现.md` |
| 开发测试 | ✅ | `competition/04-开发测试与部署.md` |
| 应用价值 | ✅ | `competition/05-应用价值与展望.md` |
| 附录材料 | ✅ | `competition/06-附录-代码与测试数据.md` |
| 演示脚本 | ✅ | `competition/演示脚本.md` |
| 测试报告 | ✅ | `competition/测试报告.md` |
| 一键启动 | ✅ | `start.bat` + `启动网页.bat` |

---

## 八、开发历程

| 阶段 | 内容 | Commits |
|------|------|:---:|
| Phase 1 | 18 结构核心（Agent/工具/Provider/记忆/Hook/权限/子代理/配置/CLI） | 13 |
| Phase 1.5 | 13 增强（Anthropic/恢复/压缩/Fleet/Checkpoint/TUI/MCP/缓存/Pool） | 10 |
| Phase 2 | 通道系统 + 插件生态 | 3 |
| Phase 3 | 自改进 + 部署矩阵 | 4 |
| Phase 4 | 通道矩阵 + Learning Graph | 2 |
| 审查修复 | 3 轮审查 → 36 bugs 修复(7 CRITICAL + 14 HIGH + 15 MEDIUM) | 8 |
| 竞赛冲刺 | 文档 + DeVET 融入 + 不足修复 | 6 |

---

## 九、关键文件修改指南

### 添加新工具
1. 创建 `internal/tool/builtin/newtool.go`，实现 `tool.Tool` 接口（5 方法）
2. 在 `registry.go` 的 `RegisterAll` 中注册

### 添加新 Provider
1. 创建 `internal/provider/newprov/newprov.go`
2. 实现 `provider.Provider` 接口（`Stream` 方法）
3. 在 `boot.go` 的 `Build()` 中添加 `case` 分支

### 修改 System Prompt
编辑 `internal/boot/boot.go` 的 `buildSystemPrompt()` 函数

### 修改 Web UI
编辑 `internal/serve/chat.go` 的 `chatHTML` 变量（内嵌 HTML/CSS/JS）

---

## 十、环境变量参考

| 变量 | 用途 | 必需 |
|------|------|:---:|
| `DEEPSEEK_API_KEY` | DeepSeek API Key | ✅ |
| `ANTHROPIC_API_KEY` | Anthropic API Key | — |
| `BOUNTY_AUTH_TOKEN` | Web 控制台认证 | — |
| `BOUNTY_HOME` | 数据目录（默认 `~/bounty-data`） | — |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | — |

---

## 十一、依赖项

### Go 依赖 (go.mod)
```
github.com/BurntSushi/toml      ← TOML 配置解析
github.com/charmbracelet/bubbletea ← TUI 框架
github.com/charmbracelet/lipgloss   ← TUI 样式
github.com/mattn/go-sqlite3     ← SQLite 驱动
gopkg.in/yaml.v3                ← YAML 解析
```

### 外部依赖
- Python 3.10+ (DeVET 后端)
- Chrome/Chromium (browser 工具，可选)
- Docker Desktop (Docker 沙箱，可选)
- ripgrep/rg (grep 工具加速，可选)

---

## 十二、许可证与引用

Bounty 使用 MIT 协议。融合了以下开源项目的架构设计：

- [OpenClaw](https://github.com/openclaw/openclaw) — 多通道架构、工具所有权模型
- [Claude Code](https://github.com/anthropics/claude-code) — Hook 系统、插件组件模型
- [Hermes Agent](https://github.com/NousResearch/hermes-agent) — 自改进闭环、KawaiiSpinner
- [Reasonix](https://github.com/esengine/DeepSeek-Reasonix) — 缓存稳定性、Fleet 子代理

DeVET 是独立研究项目（潘怀宇，江苏警官学院），投《信息安全学报》和中国全智赛 AI+软件创新赛道。

---

## 十三、2026-08-05 全面修复升级记录

> 本轮针对 3 轮审查遗留问题 + 未接线功能做全面修复升级，全部验证通过（`go build` / `go vet` / `go test` 除 desktop 实验依赖外全绿）。

### P0 安全修复（含测试）
- `agent.go`：Ask 决策真正调用 `Asker.Ask`（无 Asker 则拒绝，不再静默放行）；Deny 直接报错
- `cmd/bounty/main.go`：新增 `authMiddleware`，`/`、`/dashboard`、`/export`、`/events` 均受 `BOUNTY_AUTH_TOKEN` 保护
- `remember.go`：`sanitizeFilename` 防路径穿越（`../evil` → `--evil.md`）
- `web_fetch.go`：SSRF 防护（RFC1918 显式 `IsPrivate()`）、15s 超时、UTF-8 安全截断
- `defaults.go`：移除 `"*"` 通配、补 Windows 命令白名单与危险命令黑名单
- `boot.go`：`guardianGate` — yolo 模式下危险命令/敏感文件升级为 Ask

### P1 正确性修复
- `mcp/client.go`：响应读取改为单 reader goroutine + pending 路由（消除 ctx 取消后 goroutine 泄漏 + 响应 ID 校验）；handshake/discover 挂 30s 超时；`Host.Close` 清空 clients
- `telegram.go`：去 ticker 改单 goroutine 阻塞长轮询（timeout=30）；处理完才推进 offset（失败可重试）；补 ParseInt/sendMessage/网络错误日志
- `browser.go`：CDP 命令从错误的 `POST /json/protocol/<id>` 改为走 `webSocketDebuggerUrl`（内置最小 RFC6455 WS 客户端，零新依赖）；过滤扩展 target；独立临时 profile + 端口占用检查；启动等待 5s 轮询（实测 navigate/content/screenshot/click 全通过）
- 重试 panic（`fn == nil`）、Anthropic/Ollama 错误分类补 Backoff、UTF-8 截断、nil map 兜底等

### P2 接线升级
- `loader.go`：`mergeConfig` 改字段级合并，Hooks/Language/Version 不再丢失（Hook 系统此前完全不可达）
- `boot.go`：`PostToolUse` 补 Fire；接线 `Insights`/`Reviewer`/`Checkpointer`/`Asker`（TUI/run 用 `TerminalAsker`）
- `gateway.go`：SSE sink 经 `event.Fanout` 注册到 controller（`AddSink`/`RemoveSink`）
- `environment.go`：`probeCmd` 2s 超时，防止探针挂起 Build
- `doctor`：硬编码 "Builtin tools: 19" 修正为动态口径 20（12 builtin + 5 DeVET + 3 子代理）

### P3 文档口径统一
- 统一为：**20 工具**（12 内置 + 5 DeVET + 3 子代理）、**37 commits**、**91 Go 文件**、**36 包**、**32 测试函数**、**~29MB 产物**
- `docs/comparison.md` 全面更新过期功能标记（OpenAI/Ollama/所有权模型/记忆/插件/通道/部署等）

---

## 十四、2026-08-05 安全加固（对应 `docs/security-report-2026-08-05.md`）

> 按审计报告 H1/H2/M1-M4/L1-L5 逐项修复，验证：`go build` ✅、`go vet` ✅、`go test` 除 desktop 实验依赖外全绿（新增 permission 单测 9 个）。

### 高危
- **H1 通道认证**：新建共享 `internal/auth` 包（`TokenFromRequest` + `Middleware`，`crypto/subtle` 恒定时间比较）；`cmd/bounty/main.go` 5 端点、`internal/channel/gateway.go` 5 端点、`internal/channel/httpapi/httpapi.go` 2 端点全部接入 `auth.Middleware`；Gateway 与 HTTP API 默认绑定 `127.0.0.1`（不再暴露到局域网/公网）
- **H2 文件系统保护**：新增 `internal/permission/paths.go` 路径策略 helper（`~` 展开 + 绝对化 + 大小写不敏感 + 目录前缀语义）；`ForbidRead` 接线到 `read_file`/`grep`/`glob`/`code_index`（在 `Gate.Check` 统一拦截）；`isForbidWrite` 重写，`Windows/*`、`System32/*`、`~/.ssh/*` 等默认模式现在真正匹配绝对路径；`NewGate` 增加 sandbox 参数；新增 9 个单测

### 中危
- **M1 XSS**：`dashboard.go` session 列表/事件流改 `createElement`+`textContent`；`chat.go` 4 处 innerHTML 拼接加 `esc()` HTML 转义
- **M2 HTTP 超时**：main.go/Gateway/HTTPAPI 统一 `ReadHeaderTimeout=10s`、`ReadTimeout=30s`、`WriteTimeout=120s`、`IdleTimeout=60s`、`MaxHeaderBytes=1MiB`；`/chat/api/send`、`/webhook/`、`/api/message` 加 `MaxBytesReader` 1MiB；SSE 用 `ResponseController` 逐写保活
- **M3 认证比较**：全部改为恒定时间比较（`auth` 包 + `ChatHandler` 内部兜底）
- **M4 白名单名称**：`defaults.go` Allow.Tools 改为真实工具名（`read_file` 等）；`NewGate` 增加 camelCase→snake_case 规范化兼容旧配置

### 低危
- **L1**：bash 超时钳制到 `[1s, 600s]`
- **L2**：Dockerfile 非 root（`bounty` 用户）、默认 `CMD ["serve"]`、`/health` 健康检查
- **L3**：ssh/scp `StrictHostKeyChecking=accept-new` → `yes`（要求 known_hosts，防 MITM）
- **L4**：deny 模式匹配前规范化 `-f`→`--force`、`--force-with-lease`→`--force`（`git push -f` 不再绕过）
- **L5**：web_search 域名过滤改主机名边界匹配（`example.com` 不再误匹配 `evil-example.com`）；DeVET `isRunning` 探测客户端加 2s 超时

### 遗留说明
- `govulncheck` 未集成：当前环境 `proxy.golang.org` 不可达无法安装依赖扫描器，建议 CI 补上
- yolo 模式下 `read_file` 读 `.env` 不拦截（guardian 仅覆盖 bash/write/edit）——信任模式设计权衡，`ForbidRead` 配置可覆盖
- `store/sqlite.go` `LIKE %query%` 通配符扩大匹配范围：非注入问题，未改动

---

## 十五、2026-08-05 论文融入（BIPIA 边界标记 + 注入扫描加强）

> 依据 `docs/paper-integration-2026-08-05.md`（17 篇 CCS '25/KDD '25/ASIA CCS '26 论文融入分析），先落地前 2 项：验证：`go build` ✅、`go vet` ✅、`go test` 全绿（新增 9 个单测）。

### BIPIA 不可信内容边界标记
- `web_fetch.go`：抓取内容统一包裹 `<data url="...">…</data>` 边界（转义内容中的 `</data>` 防提前闭合）；命中注入/自复制标记的页面额外附 `[SECURITY]` 警告但仍在边界内返回（标记是防御，不阻断合法抓取）
- `loader.go`：`Doc` 新增 `InjectionHits` 字段，4 级记忆加载时扫描；`boot.go` `buildSystemPrompt` 对有标记的记忆文档用 `<data source=... warning=...>` 边界渲染，让模型按"数据"而非"指令"处理

### 注入扫描加强（RAGworm/DonkeyRail + Mind the Web）
- `injection.go`：`InjectionPatterns` 从 17 条扩到 28 条（`ignore all instructions`、`disregard`、`system instruction:`、`from now on`、任务对齐注入 `to complete this task,` 等）
- 新增 `SelfReplicationPatterns`（16 条自复制传播特征：`copy and paste this`、`forward this message`、`add this to your memory` 等）+ `ScanSelfReplication` / `ScanAll` / `IsSafeAll`
- `remember.go`：写入记忆前改走 `IsSafeAll`（同时拦注入与自复制蠕虫）
- 测试：injection 5 个 + loader 2 个 + web_fetch 边界 2 个

---

## 十六、2026-08-05 论文融入（CoT 泄露脱敏）+ 论文复现套件

> 依据 `docs/paper-integration-2026-08-05.md` 第 3 项落地；并为已融入的 4 篇论文建复现套件。验证：`go build` ✅、`go vet` ✅、`go test ./repro/ -v` 7/7 ✅。

### CoT 推理泄露脱敏
- `internal/memory/leak.go` 新增：`SensitivePatterns`（`sk-...`、AWS `AKIA`、GitHub `ghp_`、Slack `xox*`、PEM 私钥、`password=`/`api_key=`/`secret=` 赋值）+ `ScanSensitive` / `RedactSensitive`
- `internal/event/event.go`：`Fanout` 新增 `Redact func(string) string` 钩子，广播前对 `TextDelta`/`ReasoningDelta` 脱敏——覆盖 console/SSE/TUI/dashboard 全部前端，持久化走独立路径不受影响
- `internal/boot/boot.go`：`fanout.Redact = memory.RedactSensitive` 接线
- 测试：leak 4 个 + fanout 2 个

### 论文复现套件（`repro/repro_test.go`，7 用例）
- **BIPIA**（KDD '25）：恶意网页注入 → 检出 + `<data>` 边界包裹 + `</data>` 防逃逸（2 用例）
- **RAGworm/DonkeyRail**（CCS '25）：自复制 prompt → `remember` 拒绝（2 用例）
- **Mind the Web**（ASIA CCS '26）：任务对齐注入 → 检出（1 用例）
- **CoT Leakage**（ASIA CCS '26）：推理/文本流密钥 → fanout 脱敏（2 用例）
- 报告：`docs/repro-papers-2026-08-05.md`
- `builtin.WrapDataBoundary` 由 `wrapDataBoundary` 导出（供复现套件调用）
