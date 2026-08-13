# Bounty vs 参考项目 — 全功能对比

> Bounty 37 commits · 91 Go files · 36 packages · 全部代码亲手实现

## 一、核心 Agent 循环

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 对话循环引擎 | ✅ agent.go | ✅ | 闭源 | ✅ 5780行 | ✅ 600行 |
| 流式响应处理 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 中断/取消 | ✅ context | ✅ | ✅ | ✅ | ✅ |
| 多轮迭代控制 | ✅ maxSteps | ✅ | ✅ | ✅ IterationBudget | ✅ maxSteps+grace |
| 工具调用分发 | ✅ 只读并行 | ✅ | ✅ | ✅ 顺序/并发 | ✅ |
| 错误分类/重试 | ✅ 8类错误 | ✅ | ✅ | ✅ 74KB | ✅ storm检测 |
| Provider故障转移 | ✅ Pool轮转 | ✅ | ✅ | ✅ 122KB | ✅ |
| 护栏(循环检测) | ✅ stormSig | ✅ | ✅ | ✅ 3种护栏 | ✅ |
| 工具调用修复 | ✅ 修复+回喂重试 | ✅ | ✅ | ✅ JSON修复 | ✅ repair包 |
| Final Readiness Gate | ❌ | ❌ | ❌ | ❌ | ✅ |

## 二、工具系统

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| Tool接口 | ✅ 5方法 | ✅ Descriptor | ✅ | ✅ registry | ✅ 5方法 |
| Registry | ✅ 缓存Schema | ✅ | ✅ | ✅ | ✅ |
| 工具所有权模型 | ✅ 4种归属 | ✅ 4种归属 | ❌ | ❌ | ❌ |
| 只读标记 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 并行执行 | ✅ 只读并行 | ✅ | ✅ | ✅ 8线程 | ✅ |
| 预览(Previewer) | ❌ | ❌ | ❌ | ❌ | ✅ |
| 输出裁剪 | ✅ 32KB | ✅ | ✅ | ✅ 动态 | ✅ 32KB |
| 内置工具数 | 20 | 25+ | 15+ | 30+ | 20+ |

**Bounty内置工具**: bash, read_file, write_file, edit_file, grep, glob, web_fetch, web_search(DDG), todo_write, code_index, remember, browser, task, read_only_task, fleet + 5 个 devet_* 工具

## 三、模型/Provider

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| DeepSeek | ✅ OpenAI兼容 | ✅ | ❌ | ✅ | ✅ 主要 |
| Anthropic | ✅ Messages原生 | ✅ | ✅ 原生 | ✅ | ✅ |
| OpenAI | ✅ openai_native | ✅ | ✅ | ✅ | ❌ |
| Ollama | ✅ | ✅ | ❌ | ✅ | ❌ |
| Provider数 | 4 | 30+ | 1(主要) | 32+ | 2 |
| Credential Pool | ✅ 轮转+耗尽 | ✅ | ✅ | ✅ 122KB | ❌ |
| 错误8分类 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 提示词缓存 | ✅ 8层 | ✅ | ✅ | ✅ cache_control | ✅ 8层 |
| 缓存诊断 | ✅ PrefixShape | ✅ | ✅ | ❌ | ✅ |
| 双模型Coordinator | ❌ | ❌ | ❌ | ❌ | ✅ |
| MoA多模型讨论 | ❌ | ❌ | ❌ | ✅ 60KB | ❌ |

## 四、记忆/持久化

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| SQLite存储 | ✅ WAL模式 | ✅ 双DB | ❌ | ✅ | ✅ |
| FTS5全文搜索 | ✅ | ❌ | ❌ | ✅ | ❌ |
| 会话持久化 | ✅ SaveTurn | ✅ | ✅ | ✅ | ✅ |
| 会话恢复 | ✅ --resume | ✅ | ✅ | ✅ | ✅ |
| 层次化记忆 | ✅ 4级加载 | ✅ | ❌ | ✅ MEMORY.md | ✅ 4级 |
| 自动记忆 | ✅ remember | ✅ | ❌ | ✅ remember | ✅ |
| 记忆漂移检测 | ✅ drift.go | ❌ | ❌ | ✅ | ❌ |
| 注入扫描 | ✅ injection.go | ❌ | ❌ | ✅ | ❌ |
| Honcho用户建模 | ❌ | ❌ | ❌ | ✅ | ❌ |

## 五、插件/扩展

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 插件清单 | ✅ TOML | ✅ JSON | ✅ JSON | ✅ register() | ✅ MCP Spec |
| 技能系统 | ✅ YAML+索引 | ✅ | ✅ 3级加载 | ✅ 20分类 | ✅ |
| 命令系统 | ✅ TOML | ✅ | ✅ .md | ✅ | ✅ |
| Agent定义 | ✅ TOML | ✅ | ✅ .md | ✅ | ❌ |
| 插件发现 | ✅ 多源扫描 | ✅ | ✅ | ✅ | ❌ |
| MCP集成 | ✅ stdio | ✅ | ✅ mcp__* | ✅ 263KB | ✅ 完整 |
| 技能安全扫描 | ❌ | ❌ | ❌ | ✅ AST审计 | ❌ |
| 技能生命周期 | ✅ curator | ❌ | ❌ | ✅ 自动变迁 | ❌ |
| 插件市场 | ❌ | ✅ ClawHub | ✅ marketplace | ✅ SkillsHub | ❌ |

## 六、通道/消息

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 多通道支持 | ✅ 5通道 | ✅ 20+ | ❌ CLI only | ✅ 20+ | ❌ 3 bot |
| Channel接口 | ✅ 12方法 | ✅ 30+slot | ❌ | ✅ | ❌ |
| Telegram/Discord/Slack | ✅ Telegram | ✅ | ❌ | ✅ | ❌ |
| WhatsApp/Signal | ❌ | ✅ | ❌ | ✅ | ❌ |

## 七、Hook/生命周期

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| Hook事件数 | 9 | 8 | 9 | 6 | 5 |
| PreToolUse | ✅ | ✅ | ✅ Allow/Deny | ✅ | ✅ |
| PostToolUse | ✅ | ✅ | ✅ | ✅ | ✅ |
| Stop | ✅ | ❌ | ✅ Block | ❌ | ✅ |
| SubagentStop | ✅ | ❌ | ✅ | ❌ | ❌ |
| UserPromptSubmit | ✅ | ❌ | ✅ | ❌ | ❌ |
| SessionStart/End | ✅ | ✅ | ✅ | ❌ | ❌ |
| PreCompact | ✅ | ✅ | ✅ | ❌ | ❌ |
| Notification | ✅ | ❌ | ✅ | ❌ | ❌ |
| Shell模式 | ✅ | ✅ | ✅ | ✅ | ✅ |
| Prompt模式 | ❌ | ❌ | ✅ | ❌ | ❌ |

## 八、安全/权限

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 4种姿态 | ✅ ask/auto/yolo/plan | ✅ | ✅ | ✅ | ✅ |
| Bash模式匹配 | ✅ 白+黑名单 | ✅ | ✅ | ✅ | ✅ |
| 文件系统保护 | ✅ forbid_write | ✅ | ✅ | ✅ | ✅ sandbox |
| OS沙箱 | ✅ API密钥剥离 | ✅ | ✅ Bash sandbox | ✅ Docker等 | ✅ Landlock |
| 3层配置 | ❌ | ❌ | ✅ Enterprise | ❌ | ❌ |
| Guardian守护 | ✅ yolo审查 | ❌ | ❌ | ❌ | ✅ |
| Safe Mode | ❌ | ❌ | ❌ | ❌ | ✅ |
| 企业MDM | ❌ | ❌ | ✅ | ❌ | ❌ |

## 九、子代理/委托

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 单个子代理 | ✅ task | ✅ ACP | ✅ Task | ✅ delegate | ✅ task |
| 只读子代理 | ✅ read_only_task | ✅ | ✅ | ❌ | ✅ |
| 并行子代理 | ✅ fleet(2-64) | ✅ | ✅ | ✅ async | ✅ fleet |
| 上下文隔离 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 深度限制 | ✅ maxDepth=2 | ✅ | ✅ | ✅ | ✅ 2 |
| 路径冲突检测 | ✅ preflight | ❌ | ❌ | ❌ | ✅ |
| 写者调度 | ✅ sem(3) | ❌ | ❌ | ❌ | ✅ scheduler |
| 工具过滤 | ✅ 自动剥离 | ✅ | ✅ | ✅ | ✅ |

## 十、配置系统

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 配置格式 | TOML | JSON | JSON | YAML | TOML |
| 多级解析 | ✅ 项目>用户>默认 | ✅ | ✅ 3层 | ✅ profile | ✅ 3层 |
| 默认值 | ✅ | ✅ | ✅ | ✅ | ✅ |
| Secret保护 | ✅ env only | ✅ | ✅ | ✅ 1Password | ✅ |
| 快照修复 | ✅ SafeLoad | ✅ doctor | ❌ | ✅ | ✅ |
| Doctor诊断 | ✅ doctor | ✅ | ❌ | ✅ 116KB | ✅ |

## 十一、CLI/UI

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| CLI chat | ✅ Bubbletea | ✅ | ✅ | ✅ 751KB | ✅ Bubbletea |
| CLI run | ✅ 单次执行 | ✅ | ✅ | ✅ | ✅ |
| CLI doctor | ✅ 诊断 | ✅ | ❌ | ✅ | ✅ |
| 会话列表 | ✅ --list | ✅ | ✅ | ✅ | ❌ |
| 会话恢复 | ✅ --resume | ✅ | ✅ | ✅ | ❌ |
| Web仪表盘 | ✅ dashboard | ✅ React | ❌ | ✅ Vite+TS | ✅ 嵌入SPA |
| 桌面应用 | ✅ Wails实验 | ✅ 原生 | ❌ | ✅ Tauri | ✅ Wails |

## 十二、上下文管理

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| Token精确压缩 | ✅ 3级(软/紧/强)+模型摘要 | ✅ | ✅ | ✅ ContextCompressor | ✅ compact.go |
| System Prompt缓存 | ✅ 构建后不变 | ✅ | ✅ | ✅ 3层prompt | ✅ 8层保护 |
| Skills索引限制 | ✅ 4000字 | ✅ | ✅ 3级加载 | ❌ | ✅ 4000字 |
| Turn-tail注入 | ✅ compose() | ✅ | ✅ | ❌ | ✅ |
| Checkpoint/Rewind | ✅ git-free | ❌ | ❌ | ❌ | ✅ |
| 环境探针缓存 | ✅ sync.Once | ✅ | ❌ | ❌ | ✅ |
| 崩溃恢复 | ✅ checkpoint | ✅ | ❌ | ✅ metadata | ✅ |

## 十三、自改进/学习

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| Background Review | ✅ 反思+自动记忆落库 | ❌ | ❌ | ✅ daemon线程 | ❌ |
| Skill Nudge | ✅ | ❌ | ❌ | ✅ | ❌ |
| Curator调度 | ✅ curator | ❌ | ❌ | ✅ 7天周期 | ❌ |
| 技能自动创建 | ❌ | ❌ | ❌ | ✅ | ❌ |
| Learning Graph | ✅ | ❌ | ❌ | ✅ | ❌ |

## 十四、部署/基础设施

| 功能 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 单二进制 | ✅ go build | ❌ | ❌ npm包 | ❌ Python | ✅ CGO=0 |
| Docker | ✅ Dockerfile | ✅ 21KB | ✅ DevContainer | ✅ 20KB | ❌ |
| Homebrew | ✅ 公式 | ✅ | ✅ | ✅ | ✅ |
| npm分发 | ✅ npm/ | ❌ | ✅ | ❌ | ✅ |
| systemd | ✅ 单元文件 | ✅ | ❌ | ✅ | ❌ |
| K8s | ❌ | ✅ | ❌ | ❌ | ❌ |
| SSH远程 | ✅ ssh.go | ❌ | ❌ | ✅ | ✅ |

## 十五、特性覆盖率总览

| 类别 | Bounty | OpenClaw | Claude Code | Hermes | Reasonix |
|------|:---:|:---:|:---:|:---:|:---:|
| 核心Agent循环 | 🟢 8/10 | 🟢 9/10 | 🟢 闭源 | 🟢 10/10 | 🟢 10/10 |
| 工具系统 | 🟢 7/8 | 🟢 8/8 | 🟢 7/8 | 🟢 8/8 | 🟢 8/8 |
| Provider | 🟢 9/10 | 🟢 10/10 | 🟡 5/10 | 🟢 10/10 | 🟡 7/10 |
| 记忆/持久化 | 🟢 8/9 | 🟡 6/9 | 🟡 2/9 | 🟢 9/9 | 🟡 5/9 |
| 插件/扩展 | 🟢 7/9 | 🟢 9/9 | 🟢 9/9 | 🟢 9/9 | 🟡 4/9 |
| 通道/消息 | 🟢 3/4 | 🟢 4/4 | 🔴 1/4 | 🟢 4/4 | 🔴 1/4 |
| Hook/生命周期 | 🟢 8/9 | 🟡 6/9 | 🟢 9/9 | 🟡 4/9 | 🟡 4/9 |
| 安全/权限 | 🟡 5/8 | 🟡 5/8 | 🟢 8/8 | 🟡 6/8 | 🟢 8/8 |
| 子代理 | 🟢 7/7 | 🟡 5/7 | 🟡 5/7 | 🟡 5/7 | 🟢 7/7 |
| 配置 | 🟢 6/6 | 🟢 6/6 | 🟡 3/6 | 🟢 6/6 | 🟢 6/6 |
| CLI/UI | 🟢 6/7 | 🟡 5/7 | 🟡 3/7 | 🟢 7/7 | 🟢 7/7 |
| 上下文管理 | 🟢 7/7 | 🟡 5/7 | 🟡 3/7 | 🟡 5/7 | 🟢 7/7 |
| 自改进 | 🟢 4/5 | 🔴 0/5 | 🔴 0/5 | 🟢 5/5 | 🔴 0/5 |
| 部署 | 🟢 7/8 | 🟢 6/8 | 🟡 3/8 | 🟢 7/8 | 🟡 4/8 |
| **总计** | **86/107** | **86/107** | **63/107** | **95/107** | **77/107** |

🟢 ≥80% | 🟡 50-79% | 🔴 <50%

## Bounty 的独特优势

| 维度 | 优势 |
|------|------|
| **代码质量** | 102个Go文件，全手写，零依赖膨胀(Bubbletea除外)，41次独立commit |
| **架构清晰** | 39个单一职责package，依赖方向单一，接口边界明确 |
| **编译产物** | `CGO_ENABLED=0 go build` → ~17MB 静态单二进制，无运行时依赖 |
| **子代理系统** | Fleet是唯一同时实现并行+路径冲突检测+写者调度的 |
| **Hook覆盖** | 9事件全覆盖，唯一同时覆盖Stop/SubagentStop/Notification的 |
| **上下文管理** | 唯一同时有Token压缩+Checkpoint+环境探针+缓存诊断的 |

## Bounty 需要追赶的方向

| 优先级 | 方向 | 现状 |
|--------|------|------|
| ✅ 已完成 | 通道/消息系统 | 5 通道（Terminal/Webhook/HTTP API/Telegram/Gateway SSE） |
| ✅ 已完成 | 自改进/学习 | 后台反思 + Skill Nudge + Curator + Learning Graph |
| 🟡 进行中 | 插件生态 | Commands/Agents 已支持；技能安全扫描、插件市场待补 |
| ✅ 已完成 | 部署矩阵 | Docker/Homebrew/npm/systemd/SSH 均已提供 |
| ✅ 已完成 | Provider矩阵 | 4 个（DeepSeek/Anthropic/Ollama/OpenAI native） |
| ✅ 已完成 | Web UI | dashboard 仪表盘 + Wails 桌面（实验） |
