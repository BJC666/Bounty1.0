# Bounty → Claude Code 高度：差距分析与实施路线图

> 版本：v1.0 | 日期：2026-08-13 | 状态：待评审
> 依据：Bounty1.0 @ 6bd87c4 逐包代码勘察 + Claude Code 2026-08 能力面
> 配套：`docs/comparison.md`（功能清单对比）、`docs/specs/2026-07-20-bounty-agent-design.md`（系统设计）

---

## 0. 结论先行

1. **功能面上 Bounty 已覆盖 Claude Code 约 85% 的清单**，但"高度"不由清单决定，由 5 个工程维度决定：上下文工程、执行可靠性、可测量性（Eval）、模型侧适配、体验与生态。
2. **当前最伤体验的 5 个短板**（按杠杆排序）：压缩丢消息、记忆只写不读、编辑工具无漂移自愈、工具调用无 JSON 修复、没有 Eval 体系。
3. **路线**：8 周、4 个阶段（Eval 基线 → 上下文工程 → 执行可靠性 → 安全生态与差异化），每个工作项带验收标准，全部可回归。
4. **定位建议**：不与 Claude 本体硬拼模型质量，打"**DeepSeek/Qwen 底座 + Claude Code 形态 + DeVET 可验证多代理安全**"——安全验证是 Claude Code 没有的护城河。

---

## 1. 什么才叫"达到 Claude Code 的高度"

### 1.1 四个可测量指标

| 指标 | 定义 | 基线（待阶段 0 跑出） | 8 周目标 |
|------|------|------|------|
| 任务成功率 pass@1 | 自有 30 任务 Eval 一次通过率 | qwen3.8-max **96.7%**（A 10/10、B 9/10、C 10/10，详见 `docs/eval-baseline.md`）；deepseek-v4-pro 待有效 key 补测 | 基线的 1.5–2 倍，且 ≥50% |
| 完成效率 | 每任务平均步数 / token / 费用 | qwen3.8-max：5.6 步 / 21,031 入 tok / 1,303 出 tok | token/任务 ↓40% |
| 长程稳定性 | 死循环率、工具调用失败率、崩溃率 | qwen3.8-max：工具失败率 7.3%、超时 0、护栏退出 1/30 | 死循环 <2%，工具失败 <5% |
| 信任感 | 权限拦截可解释、文件可回滚、输出可复现 | 已部分具备 | 每项有自动测试 |

> 原则：**没有数字的"变好了"不成立**。每个阶段结束都要有对比数字。

### 1.2 为什么功能清单比不出高度

`docs/comparison.md` 的对照表把功能项打勾，但用户体验来自细节：

- Claude Code 的 `apply_patch` 在文件被外部改动后仍能靠上下文锚点命中；Bounty 的 `edit_file` 只做精确唯一匹配，一次失败就整轮浪费。
- Claude Code 的压缩是**模型生成的摘要**，保留"改了什么、为什么"；Bounty 的压缩是**直接丢消息**并插入一行假摘要文本。
- Claude Code 背后有内部 Eval 体系保证每次改版不倒退；Bounty 目前只有 97 个单元测试，没有任何端到端任务回归。
- 清单上两者都"有 Todo"，但 Claude Code 的 todo 有宿主状态、UI 展示、驱动多轮执行；Bounty 的 `todo_write` 无状态、不入上下文。

所以本路线图只列"能改变测量指标的工程"，不再重复清单。

---

## 2. 现状盘点（2026-08-13 逐包勘察）

### 2.1 已经达标的（保持，不返工）

- **Agent 循环**（`internal/agent/agent.go`）：流式 reasoning/text/tool-call 增量合并、只读工具并行、8 类错误分类重试+退避、storm 信号与断轮检测、检查点挂钩。
- **子代理**（`internal/agent/task.go`、`fleet.go`）：`task`/`read_only_task`/`fleet` 三形态，子代理隔离上下文、剥离递归委托与作业控制工具、深度限制、fleet 2–64 并行+写路径信号量。
- **安全面**（`internal/permission`、`guardian`、`sandbox`、`secrets`、`memory/leak.go`）：4 姿态权限门、危险命令拦截、API Key 剥离与轮转、CoT 泄露正则脱敏、记忆注入扫描（`memory/injection.go`）——这块是超过多数开源代理的。
- **扩展面**（`internal/hook`、`mcp`、`skill`、`plugin`、`channel`）：9 事件 Hooks（与 Claude Code 对齐）、MCP stdio 客户端、技能 frontmatter+索引+Curator 生命周期、TOML 插件、Telegram/Webhook/Terminal/HTTP 5 通道。
- **工程面**：4 Provider（Anthropic 原生/OpenAI 兼容/OpenAI 原生/Ollama）、PrefixShape 缓存命中诊断、SQLite+FTS5、会话 resume、Docker 沙箱可选、Wails 桌面 + Web 控制台、CI。
- **差异化**：DeVET 委托链验证（7 项递归检查、8 类攻击检测、归因）。

### 2.2 十二个关键短板（按杠杆排序，每条=一个工作项）

| # | 短板 | 代码证据 | 影响 |
|---|------|----------|------|
| S1 | 压缩=丢消息，非摘要 | `internal/agent/compact.go` 插入占位文本 `[Earlier conversation has been summarized...]` 但从未调用模型摘要；且 `MaxContext: 200000` 硬编码，不读 provider 的 `context_window`（qwen=128000 会溢出） | 长任务中途失忆、上下文成本失控，是最伤体验的一项 |
| S2 | 记忆只写不读 | `internal/tool/builtin/remember.go` 写入 `RememberStore`，但 `internal/boot/boot.go` 的 `buildSystemPrompt` 只注入 BOUNTY.md/AGENTS.md 静态文档，无记忆检索工具、无注入 | "记住的偏好"下一会话就消失，自改进闭环断裂 |
| S3 | 后台反思不回灌 | `agent.go` Run 循环里 reviewer 结果仅 `notification` 事件，不进会话、不触发 remember | 学习系统只输出日志，不产生任何行为改变 |
| S4 | 无 Repo Map | `internal/tool/builtin/code_index.go` 是按需 regex 符号索引，不自动注入；Claude Code 每轮注入轻量仓库地图 | 大仓库导航靠反复 grep/read，步数与 token 翻倍 |
| S5 | 编辑无漂移自愈 | `internal/tool/builtin/edit_file.go`：`old_string` 必须精确且唯一，失败只报错，不返回文件附近内容 | 一次外部改动即整轮浪费，多文件重构极易卡死 |
| S6 | 工具调用无修复 | `docs/comparison.md` 自认 ❌"工具调用修复"；Provider 无 `tool_choice` 强制、无 JSON 修复器 | DeepSeek/Qwen 输出截断或坏 JSON 时直接失败，这是国产模型的常见失败模式 |
| S7 | MCP 半套 | `internal/mcp/client.go` 明文写着 `HTTP transport not yet implemented`；无 resources/prompts、无项目/用户级配置、无 OAuth；所有 MCP 工具 `ReadOnly()=false` | 接不了远程 MCP server，权限粒度粗 |
| S8 | 沙箱无强制 | `internal/sandbox/confine.go` 注释 `Phase 1: set working directory + strip API keys`；Windows 无 Docker 时进程完全无隔离 | "安全"叙事与实际防护之间的最大落差 |
| S9 | Todo 无状态 | `internal/tool/builtin/todo_write.go` 只回显文本，无宿主状态、不入上下文 | 无法支撑多步计划执行与进度展示 |
| S10 | 无多模态 | `internal/provider/provider.go` 的 `Message` 只有 `Content string` | 无法读截图/设计稿，排障与前端任务受限 |
| S11 | 无 Eval 体系 | 仓库只有 97 个单测（22 文件），无端到端任务集、无回归曲线 | 任何"优化"无法验证，改版倒退无法发现 |
| S12 | TUI/命令面薄 | `internal/cli/tui.go` 17KB，无 slash 命令、无 diff 彩色、无工具面板 | 终端体验与 Claude Code 差距最直观 |

---

## 3. 差距矩阵（影响 × 难度 × 阶段）

| ID | 工作项 | 影响 | 难度 | 阶段 |
|----|--------|------|------|------|
| S11 | Eval 平台与基线 | ★★★★★ | 中 | 0 |
| S1 | 真摘要压缩 + context_window 读取 | ★★★★★ | 中 | 1 |
| S2 | 记忆闭环（检索+注入） | ★★★★ | 低 | 1 |
| S4 | Repo Map 自动注入 | ★★★★ | 中 | 1 |
| S5 | 锚点补丁 + 漂移自愈 | ★★★★★ | 中 | 2 |
| S6 | 工具调用 JSON 修复 + tool_choice | ★★★★ | 低 | 2 |
| S9 | Todo 宿主状态 + 计划契约 | ★★★ | 低 | 2 |
| S3 | 后台反思 → remember → 注入闭环 | ★★★ | 低 | 2 |
| S7 | MCP 补全（SSE/resources/作用域/权限标注） | ★★★ | 高 | 3 |
| S8 | Windows Job Object 沙箱 | ★★★ | 高 | 3 |
| S12a | TUI 打磨 + slash 命令 | ★★★ | 中 | 3 |
| S12b | 子代理角色化+上下文注入+摘要 | ★★★ | 中 | 3 |
| S10 | 多模态输入 | ★★ | 中 | 4 |
| — | 检查点升级为 git 影子仓库 | ★★★ | 中 | 3 |
| — | 内置技能 20 个 + 技能安全审计 | ★★ | 低 | 4 |
| — | DeVET 全链路接入 task/fleet | ★★★★(差异化) | 中 | 4 |
| — | 后台长命令 / run --json SDK / CI 示例 | ★★ | 低 | 4 |

---

## 4. 分阶段路线图（8 周）

> 工时按单人全职估算；每项验收=可以跑的命令/可以读到的产物。执行顺序依赖图中阶段 0 先行。

### 阶段 0（第 1 周）：Eval 平台与基线 —— "先能测，再谈变好"

**目标**：有一个 30 任务端到端测试集 + 一键跑分脚本 + 基线报告。

**任务集设计（30 题，3 类 × 10 题）**
- A 仓库理解：在临时 fixture 仓库（Go/TS/Python 各一，代码不经公开训练集污染的构造仓库）提问"XX 在哪里实现 / 依赖哪些模块 / 数据流是什么"。判定：答案包含 golden 关键点（脚本 grep 关键符号）。
- B 多文件改动：跨文件加功能（新增路由、加字段贯穿三层等）。判定：`go test`/`pytest`/`tsc` 全绿 + diff 不触碰禁改文件。
- C 修 bug：注入 10 个已知 bug（越界/竞态/逻辑反转）。判定：测试通过 + diff 最小（行数上限）。
- 每题限额 max_steps=50，记录：pass/fail、步数、token、费用、工具失败次数、死循环标记。

**技术要点**
- 判定器脚本化：测试命令 + 关键点检查 + diff 检查，杜绝人工打分偏差。
- 每模型一套基线：qwen3.8-max、deepseek-v4-pro 各跑 30 题，产出 `docs/eval-baseline.md`。
- CI 挂载 smoke：PR 每次跑 10 题（约 15–30 分钟），防止改版倒退。

**验收**：`docs/eval-baseline.md` 入库；`make eval-smoke` 可用；CI 绿。

**状态（2026-08-13）**：已交付——`scripts/eval/`（tasks/fixtures/solutions/runner/judge/report/selfcheck），30/30 离线自检通过；qwen3.8-max 基线 96.7% 已入库；`make eval-selfcheck/eval-smoke/eval-run/eval-judge/eval-report` 可用。deepseek-v4-pro 基线因本地 `DEEPSEEK_API_KEY` 无效暂缺，设置有效 key 后跑 `python scripts/eval/runner.py --models deepseek/deepseek-v4-pro` 即可。

### 阶段 1（第 1–2 周）：上下文工程 —— 最高杠杆

**P1-1 真摘要压缩（S1）**
- 现状：`compact.go` 直接丢消息。
- 目标：达到阈值时**用同/低档模型生成结构化摘要**（任务目标、已做决策、关键文件改动、未完成事项、错误教训），然后 `system + 摘要 + 尾部 N 条` 重建会话；摘要复用 `PreCompact` hook 通知（已有）。
- 细节：`MaxContext` 从 provider `ContextWindow` 读取；摘要本身计入下一轮缓存形状（`PrefixShape` 已有）；配 golden 测试（给定对话序列，断言摘要含关键事实、重建后不丢 todo 状态）。
- 验收：10 个长会话（200 轮模拟）压缩后，任务关键信息保留率 100%（单测断言）；token 环比下降。
- 状态（2026-08-13）：✅ 已交付——`internal/agent/compact.go` 重写为模型生成结构化中文摘要（任务目标/已做决策/关键文件改动/未完成事项/错误教训五小节），软/紧两级阈值 + 强压只保最近 2 条；`MaxContext` 改为从 provider `ContextWindow()` 读取；`CompactConfig.Summarizer/SummaryPrompt` 可注入；摘要计入下一轮缓存形状（append=hit / compact=miss）；`PreCompact` hook + notification 事件触发。验收测试 10 个全绿：`internal/agent/compact_test.go`（9 个 golden/行为测试 + 1 个端到端 `TestRunCompactsWithProviderSummaryEndToEnd`），10 个 200 轮长会话关键事实保留率 100%、二次压缩幂等；`go build ./...` 与 `go test ./...` 全绿。

**P1-2 记忆闭环（S2+S3）**
- 现状：remember 只写不读；后台反思只发通知。
- 目标：新增 `memory_search` 只读工具（FTS5 检索 remember 库）；`buildSystemPrompt` 启动时注入 Top-N（按最近使用/相关性）；后台反思结果生成"建议记忆条目"并自动落库（低风险条目）或提示用户（高风险条目）。
- 验收：跨会话测试——会话 A 记住偏好 → 会话 B 提问，断言回答用了记忆（Eval 加 3 道记忆题）。
- 状态（2026-08-13）：✅ 已交付——`memory_search` 只读工具（关键词/中文二元组评分检索，空查询列最近记忆）；`buildSystemPrompt` 启动注入最近 8 条自动记忆（注入特征条目包 `<data>` 边界）；后台反思改为结构化 JSON 抽取并自动落库（低风险条目），注入/自我复制标记条目拒绝并通知；`created` 时间戳升级 RFC3339Nano 保证排序稳定。测试全绿：`internal/memory/search_test.go`（9 项）、`memory_search_test.go`（5 项）、`background_review_test.go`（7 项）、`boot_test.go`（2 项，会话 A 写入→会话 B 系统提示注入断言）；Eval 新增 D1–D3 记忆题（judge/selfcheck 支持 D 类，33/33 自检通过）。

**P1-3 Repo Map（S4）**
- 目标：启动+文件变更时增量索引（复用 `code_index.go` 正则，加文件树与依赖边），每轮在系统提示后注入 ≤3000 token 的仓库概览；变更即失效重建。
- 验收：Eval A 类（仓库理解）步数下降 ≥30%（对照阶段 0 基线）。
- 状态（2026-08-13）：✅ 代码侧已交付——新增 `internal/repomap`（文件树+符号索引+内部依赖边，go.mod 模块路径/相对导入/顶层目录分类；指纹=路径|大小|mtime 变更即重建）；`repo_map` 只读工具强制刷新；启动注入 + 每用户轮刷新（`Options.RepoMap` 接口，`stripRepoMap` 替换而非追加，上限 10000 rune≈2500 token）；`code_index` 改为复用 repomap 单一正则源。测试全绿：`map_test.go`（7 项）、`repo_map_test.go`（4 项）、`repomap_test.go`（2 项）。验收指标（A 类步数 ↓30%）待真实模型跑分，需 QWEN token，另行执行。

**P1-4 工具结果预算（配合 S1）**
- 现状：`maxToolOut` 32KB 固定截断，无定位提示。
- 目标：分工具预算——read_file 默认 2000 行上限+offset/limit 行号提示；grep 匹配上限 300 条并提示收窄；bash 输出保留头尾各 15KB；截断信息写清"共多少、如何继续读"。
- 验收：Eval 平均 token/任务下降 ≥20%，且 pass@1 不降。
- 状态（2026-08-13）：✅ 已交付——read_file 默认 2000 行上限，截断提示含总行数/显示区间/续读 offset；grep（rg 与原生 fallback）匹配上限 300 条，提示收窄 pattern/glob/path；bash 输出保留头尾各 15000 字符，中段提示重定向续读；agent 通用 32KB 兜底截断附原始字节数。测试全绿：`truncate_test.go`（10 项）。验收指标（token/任务 ↓20%）待真实模型跑分。

**P1-5 Todo 宿主状态与计划契约（S9）**
- 目标：`todo_write` 落 SQLite；plan 姿态下系统提示强制"先输出结构化计划+todo，再动手"；每轮把当前 todo 摘要（≤10 行）注入 system 尾部；完成态变化通知 UI。
- 验收：plan 姿态 Eval 中多步任务的步数下降、完成率上升；todo 状态在 UI 可见。
- 状态（2026-08-13）：✅ 已交付——`todo_write` 重写为宿主状态源：写 SQLite `todos` 表（`ReplaceTodos`/`LoadTodos`，按 session 隔离，`schema.sql` 加表）；`boot` 注册带 store/session 的 `TodoWriteTool` 并注入 `todoSummary`（≤10 项，含 plan 姿态计划契约文本 `planContractText`）；Agent 新增 `Todos TodoSummaryProvider`，`refreshContext` 统一刷新 repo-map 与 todo 摘要，todo_write 执行后发通知事件同步 UI。测试全绿：`internal/store/todos_test.go`（7 项）、`todo_write_test.go`（7 项）、`boot_test.go`（含 plan 契约 2 项）、`repomap_test.go`（todo 摘要 2 项）。

### 阶段 2（第 3–4 周）：执行可靠性 —— 决定"能不能把活干完"

**P2-1 锚点补丁（S5）**
- 目标：`edit_file` 升级——`old_string` + 可选 `context_lines`（前后各 N 行锚点）；锚点命中但主体漂移时做空白归一化比对；仍失败时**返回目标文件附近 40 行**让模型自愈重试；`write_file` 覆盖已有文件时要求显式 `overwrite:true`。
- 验收：20 个漂移用例（文件被外部改动后重试）成功率 ≥95%；Eval B 类 pass@1 提升 ≥15%。
- 状态（2026-08-13）：✅ 已交付——`edit_file` 四级匹配：①精确（不唯一时报行号）→②空白归一化块（缩进/尾部空白/CRLF/空行漂移）→③行内空白剥离 →④全空白剥离块（如 `def f( a , b )` vs `def f(a, b)`，跨行漂移兜底）；全未命中返回「最近行上下文」自愈错误（词元命中+字符二元组相似度定位最近行，附 `重试` 提示）；`write_file` 默认拒绝覆盖既有文件，需显式 `overwrite:true`。测试全绿：`edit_file_drift_test.go` 20 个漂移用例 100% 通过（验收 ≥95%），另有精确/不唯一/上下文/覆盖守卫 8 项。Eval B 类提升待真实模型跑分。

**P2-2 工具调用修复（S6）**
- 目标：Provider 层加 `tool_choice` 强制；流式收完后校验 JSON，坏 JSON 走修复器（补尾括号/去尾逗号/修引号，借鉴 `repro/` 里 JSON 修复先例）；修复失败则把"原始输出+解析错误"回喂模型一次；schema 收紧（enum、maxLength、明确 required）。
- 验收：坏 JSON 注入测试 20 例修复成功率 ≥80%；DeepSeek/Qwen Eval 工具失败率 <5%。
- 状态（2026-08-13）：✅ 已交付——新增 `tool.RepairToolArgs`（`internal/tool/jsonrepair.go`）：尾逗号清理、补尾括号/引号（含嵌套）、单引号/弯引号归一、裸键加引号、NaN/Infinity→null、去代码围栏与前后缀杂文（按 json.Decoder 前缀截断）、非法转义（如 Windows 路径 `\U`）保留字面反斜杠，修复结果必须 `json.Valid` 才接受；Agent 流式收完后统一校验工具参数（`repairToolCallArgs`），修复失败时把「原始输出+解析错误」作为 tool 消息回喂模型重试一次（`jsonRetryUsed` 防循环）；9 个核心工具 schema 收紧（`maxLength`/`minimum`/`maximum`/`maxItems`/`additionalProperties`）。测试全绿：`jsonrepair_test.go` 28 个坏 JSON 用例 100% 修复（验收 ≥80%）、4 个不可修复用例正确报错；`internal/agent/json_retry_test.go` 验证「坏 JSON→回喂→模型重发→工具执行一次」全链路。Eval 工具失败率待真实模型跑分。注：`repro/` 中无 JSON 修复先例，按路线图自行实现。

**P2-3 错误反馈整形**
- 目标：工具错误统一返回"错误类型 + 原因 + 建议重试参数"三行格式（复用 8 类错误分类器），替换裸 error 字符串。
- 验收：Eval 中"首轮失败后自愈"比例 ≥60%（当前无此统计，先在 Eval 平台加指标）。
- 状态（2026-08-13）：✅ 已交付——新增 `tool.ShapeError`（`internal/tool/errshape.go`）：工具执行错误统一整形为「【错误类型】/【原因】/【建议重试】」三行，12 类规则分类（未知工具/权限拒绝/文件不存在/匹配不唯一/匹配未命中/路径编码/超时/网络/内容过滤/参数错误/其他，与 provider 8 类命名对齐），原因超 500 字符截断，建议重试带可操作参数（如 bash timeout 上限、edit_file replace_all）；`executeOne` 仅对 `t.Execute` 错误整形（gate/hook 错误保持原样）；Eval 平台新增「自愈率」指标：runner 记录 `first_error_step`，judge 透传，report 计算「有工具错误的任务中最终通过的比例」并加总览列。测试全绿：`errshape_test.go` 11 项、`agent/json_retry_test.go` 整形链路 1 项；selfcheck 33/33。≥60% 验收待真实模型跑分。

**P2-4 Windows 命令体验**
- 现状：`bash.go` 已做 sh→cmd 回退、GBK 解码、路径转换（此前演示中的三连错已修）。
- 目标：未知命令时返回候选提示（`pwd`→`cd`，`ls`→`dir`）；命令预检白名单；验证 `wait/bash_output/kill_shell` 在 cmd 下可用；中文路径回归测试固定。
- 验收：Windows 平台 30 条常用命令测试全绿。
- 状态（2026-08-13）：✅ 已交付——①未知命令候选提示：cmd.exe 下执行前预检，首 token 命中 24 条 Unix 别名表（ls→dir、pwd→cd、cat→type、grep→findstr、rm→del…）直接返回「请改用 Windows 等价命令」错误（不执行）；执行后若输出含「不是内部或外部命令」则追加候选/`where` 提示；`windowsNativeCommands` 白名单（31 条 cmd 内置）跳过预检，python/go/git/npm 等 PATH 命令放行。②进程树击杀：`runWithTreeKill` 重构 bash 执行路径（Start/Wait + `taskkill /PID x /T /F`），修复 cmd.exe 子进程（如 ping）孤儿化导致超时后管道悬挂 29s 的问题，实测 1.3s 返回。③中文路径回归：`bash_windows_test.go` 含中文目录/文件名回写用例。注：路线图中的 `wait/bash_output/kill_shell` 三个工具本版本并不存在，等价能力以「timeout 钳制（1s–600s）+ context 取消 + 进程树击杀」覆盖并在测试中固定。测试：新增 `bash_windows_test.go` 39 项全绿（预检 18、token 8、hint 5、路径 9、解码 3、截断 2 + 6 个 cmd 实跑），`go test ./...` 全绿。

### 阶段 3（第 5–6 周）：安全与生态

**P3-1 MCP 补全（S7）**
- 目标：SSE/HTTP transport；resources/prompts 暴露为只读工具/上下文块；项目级 `.bounty/mcp.json` + 用户级 `bounty-data/mcp.json` 配置；server 级权限标注（信任 server 的工具透传 `ReadOnly` 与审批策略）。
- 验收：连 3 个真实 MCP server（stdio×2 + SSE×1），Eval 增加 2 道 MCP 任务。
- 状态（2026-08-13）：✅ 代码侧已交付——①SSE transport（`internal/mcp/sse.go`）：GET /sse 取 endpoint 事件 → POST JSON-RPC → SSE `message` 事件按 ID 路由；②resources/prompts 暴露为只读工具（`mcp__<server>__resource_N`/`prompt_N`，prompt 参数转为 inputSchema）；③server 级权限标注：`Spec.ReadOnly/Trust`（bounty.toml `[plugins]` 加 `url/read_only/trust`），资源/提示恒只读，工具 ReadOnly 透传到权限门；④配置：`mcp.LoadSpecs` 合并用户级 `bounty-data/mcp.json` + 项目级 `.bounty/mcp.json`（同名项目覆盖用户），boot 已接线。测试：`mcp_test.go`（helper 进程假 stdio server：工具/资源/提示发现与调用 + ReadOnly 透传 2 项）、`sse_test.go`（httptest SSE 端到端 + 坏端点 2 项）、`config_test.go`（合并/覆盖/坏 JSON 3 项）全绿；Eval 新增 E1/E2 两道 MCP 任务（fixture `mcp-math` 内置 python MCP 服务器，judge 校验「文本命中 + 确实调用了 mcp__math__ 工具」），selfcheck 35/35。注：验收要求「连 3 个真实 MCP server」需外网/用户实际环境（npx/遥测服务），本轮用本地假 server（stdio 子进程 + httptest SSE）覆盖同协议路径，真实 server 连接待用户环境实测。

**P3-2 Windows Job Object 沙箱（S8）**
- 目标：bash 子进程挂 Job Object——限定可写目录（workspace + 白名单）、禁出站网络（可选开关）、子进程不可逃逸；Docker 可用时仍走容器。与 guardian 联动：危险命令在沙箱内也拦截。
- 验收：三类攻击用例（写越界路径/读敏感文件/外联）全部被隔离，`sandbox_test.go` 全绿；安全报告更新。
- 状态（2026-08-13）：✅ 已交付——①Job Object 容器（`internal/sandbox/job_windows.go`）：CREATE_SUSPENDED 启动 → 挂 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`（无 breakaway）→ 枚举线程恢复，子进程不可逃逸，容器句柄关闭即杀全家（实测 cmd 下的 ping.exe 子进程被连坐回收）；②网络开关：`Network=false` 时外联工具预检拦截（curl/pip/npm/git clone/WebClient 等 19 类模式）+ 代理环境变量毒化（HTTP(S)_PROXY→127.0.0.1:9），环境级出站阻断，kernel 级 WFP 记为后续工作；③路径策略（`policy.go`）：引号感知的重定向目标提取，写越界（workspace+allow_write 之外）与 forbid_read/forbid_write 命中的路径一律预检拦截；④guardian 联动：危险命令仍在 gate 层拦截，沙箱策略为第二道。测试：`policy_test.go` 7 项（三类攻击用例全隔离 + 无假阳性）、`job_windows_test.go` 4 项（树击杀/任务表验证/密钥剥离/代理毒化）、既有 `sandbox_test.go` 全绿。诚实边界：子进程内深路径（管道间接引用）与内核级网络阻断不在本轮范围，已在安全报告中标注。

**P3-3 检查点升级为 git 影子仓库**
- 现状：`internal/checkpoint` 文件快照，只覆盖声明了写路径的工具。
- 目标：会话开始即建影子 git 仓库自动提交工作区；每条用户消息一个 checkpoint 标签；支持"回滚到消息 N"（文件层面）。
- 验收：乱改 50 个文件后一键回滚，diff 为空。
- 状态（2026-08-13）：✅ 已交付——新增 `checkpoint.GitStore`（`internal/checkpoint/gitstore.go`）：会话启动即 `git init --bare` 影子仓库（`~/bounty-data/checkpoints/<session>/shadow.git`，位于工作区之外，不自嵌套）；每条用户消息在 `SaveTurn` 做全量快照：`git add -A -f -- .`（`-f` 连被 .gitignore 忽略的文件一并纳入，保证回滚语义精确；`:(exclude).git` 排除真实仓库；`-c core.autocrlf=false`/`core.eol=lf` 固定字节级往返）→ `--allow-empty` 提交 → `msg-<N>` 标签；`RestoreCheckpoint` = `ls-tree` 取标签文件集 → 自底向上删除树外文件（跳过 `.git`）→ `read-tree` + `checkout-index -a -f` 逐字节写回 → `update-ref` 移动 HEAD。web 端一键回滚：serve 新增 `GET /chat/api/checkpoints`、`POST /chat/api/checkpoints/restore`，chat SPA 新增「↩️ 回滚」面板（检查点列表 + 二次确认 + 状态反馈）；boot 优先注册 GitStore，git 缺失时回退旧文件 Store（旧 Store 同步补 `Restorer` 接口实现，两路径同能力）。验证：`gitstore_test.go` 8 项全绿——验收用例「乱改 50 文件 + 新增 20 未追踪 + 删 10 文件，一键回滚后与基线逐字节一致」、中间消息回滚、二进制 + 中文路径字节精确、文件↔目录交换、真实 `.git` 目录不受影响、未知消息报错；`chat_test.go` 端点 7 项（列表/恢复/缺参/不可用）；`go test ./...` 全绿、selfcheck 35/35；实测 dashboard 端到端：真实消息 → tag msg-1 → 回滚 API ok → 工作区 diff 与回滚前一致。诚实边界：回滚为文件层面（会话消息不截断）；`-f` 全量快照在大仓库下每条消息有 git 扫描成本（换取完整回滚语义）；回滚与正在运行的工具轮存在竞态（UI 尚未加轮运行锁，待 P3-5 收口）。

**P3-4 子代理增强**
- 目标：`task` 增加 `explore`/`general` 角色 prompt（explore 只读+报告结构）；父代理把"任务相关的最近上下文片段"注入子代理（≤2KB）；子代理返回结构化摘要（结论/证据/文件清单）而非原始末条全文；`model` 参数生效（子代理可换便宜模型）。
- 验收：Eval 加 5 道"必须用子代理"任务，pass@1 ≥60%；子代理输出 token 下降 50%。
- 状态（2026-08-13）：✅ 已交付——①`task` schema 增 `role`（general/explore，默认 general；explore=只读+「结论/证据/文件清单」报告结构），`read_only_task` 等价 role=explore 保留兼容；②父任务上下文注入：`selectContextSnippets` 用 CJK 二元组（滤虚词）+ 英文词元打分，从父会话选相关用户消息（得分≥2，同分取最近），总字节≤2048，注入子代理第二条 system 消息；③结构化摘要：`buildSubagentSummary` 固定三节——结论（rune 上限 1200 截断）、证据（工具名×次数）、文件清单（修改/读取，各≤15，兼容 file_path/path 两种参数键），长回答返回长度严格小于原文；④`model` 参数生效：`Agent.Options.ProvFactory` + boot 接线（provider/model 或单 provider 时裸模型名，密钥走同一 secrets 池），无 factory 时明确报错。测试：`task_test.go` 8 项全绿（上下文相关性/2KB 字节上限/无匹配为空/摘要三节与文件清单/摘要短于原文/端到端注入+摘要/缺 factory 报错/explore 只读提示/registry 剥离写工具）；`go test ./...`、`go vet` 全绿。真实模型验收：Eval 新增 E3–E7 五道"必须用子代理"任务（judge 泛化 `require_tool_prefix` 字段，E 类保留旧 mcp 判定兼容），qwen3.8-max 实测 **pass@1 = 5/5（100% ≥ 60%）**（每任务 13–26s，无超时），transcript 可见子代理按 explore 角色输出三节报告、父代理收到结构化摘要。token 下降 50% 的验收：结构化摘要结论上限 1200 rune + 三节固定格式为结构性保证（单测断言摘要严格短于长原文）；与旧版真实模型 before/after 对比因无 P3-3 前基线未跑，诚实标注。另修 report.py 空类别 KeyError 与 E 类别标签。

**P3-5 TUI 打磨 + Slash 命令（S12a）**
- 目标：bubbletea 上做键盘导航（历史/滚动）、工具调用折叠面板、diff 彩色（edit 前后对照）、权限弹窗选择；slash 命令首批：`/model`、`/compact`、`/todo`、`/export`、`/skills`、`/status`。
- 验收：录屏对比清单（导航/折叠/diff 三场景）逐项打勾。
- 状态（2026-08-13）：✅ 已交付——①键盘导航：Alt+↑/↓ 输入历史、e 展开最近工具、E 全部展开/折叠、Esc 清输入（工具弹窗中=拒绝）、↑/↓/PgUp/PgDown/Home/End 滚动（保留滚轮）；②工具折叠面板：`tool_call` 单行摘要，展开显示 args/结果（≤8 行截断）/✓✗，`edit_file` 展开显示**彩色 diff**（绿+红-，`diffLines` 前后缀裁剪+2 行上下文）；③权限弹窗：`tuiAsker` 内联对话框（数字键选择方案、Esc 拒绝、ctx 超时清空），`RunTUI` 接入 Asker；④slash 命令：`/status /model [provider/model] /compact /todo /export [文件.md] /skills /help` + 既有 `/new /switch /list /rename`；⑤支撑方法：`agent.ForceCompact()`（compact.go 拆出 `compactNow`，ForceRatio 路径+双倍超时）、`control.ForceCompact()/Skills()`、`boot.SwitchModel`（provider/model 或裸名，走 secrets 池）。新文件：`internal/cli/tui.go`（大改）、`diff.go`、`export.go`、`asker.go`、`tui_test.go`。测试：cli 16 项 + agent compact 新增 2 项 + boot SwitchModel 3 项全绿；`go vet ./...`、`go test ./... -count=1 -timeout=600s` 全绿、selfcheck 40/40；TUI 二进制实测 `bounty chat --help` / `bounty --help` / `bounty chat --list` 正常。**收尾顺带修复三处真问题**：①`chat --help` 此前直接进 TUI 在无 TTY 管道下挂起——补 help 短路 + `chatUsage`/`rootUsage`；②sandbox `TestJobCloseReapsPingChild` 与 builtin 的 ping 超时测试在全量并行时 tasklist 误判（他人 ping.exe）——子进程改用唯一命名 ping 副本 + 轮询断言；③Windows `python3.exe` Store 存根 LookPath 可见但不可执行，DeVET 后端启动失败——launcher 改为 python/python3 实测 `--version` 探测。诚实边界：验收原为"录屏对比清单逐项打勾"，键盘/折叠/diff 已由 tui_test 覆盖模型逻辑，但本机未做真实 TTY 逐键录屏（部分验收）；`/model` 切换 qwen key 实测可用，未在 TUI 内手动逐键演示全部 slash。

### 阶段 4（第 7–8 周）：差异化与口碑

**P4-1 DeVET 全链路接入（核心差异化）**
- 目标：`task`/`fleet` 每个子代理的结果自动过 DeVET 验签；攻击注入场景自动归因；web 控制台新增"验证链可视化"面板。
- 验收：Eval 加 6 道 DeVET 攻防题（8 类攻击抽样），检测率 100% 且归因正确；`competition/` 文档同步更新。
- 状态（2026-08-13）：✅ 已交付——①DeVET 后端新增 `/chain/mirror`（Bounty 真实子代理委托镜像：每子代理一条密封 DelegationGrant + CompositeProof，结果 sha256 承诺/工具调用数/写入清单绑定，root 链 notary 承诺）+ `/chain/tamper`（替换任意委托节点证明后复验，blame_path 精确到 delegation[i]→子代理名）；verify 改为使用存储根 AID（scenario 与 mirror 两路通用）。②Bounty 侧 `devet.MirrorClient`（MirrorAndVerify/Verify/Tamper/State 快照，10s 超时）；`Agent.Options.DeVET` 挂钩——runChildAgent 完成后自动镜像验签，【DeVET 验证】节追加进子代理摘要（✅ 真实有效/❌ 检出故障+归因/⚠️ 后端不可用），后端失败绝不阻断父任务（纵深防御非硬门）；`event.DeVETEvent` 广播。③web 控制台新增「🛡️ 验证链」面板（GET /chat/api/devet/state + SSE devet_verify 实时刷新）：宿主→各子代理承诺/工具数/写入文件/逐节点 ✓✗/总体判定/归因路径。④Eval F1–F6（8 类攻击抽样：A1/A2/A4/A7/A8/A10）：judge/selfcheck/report 支持 F 类（require_tool_prefix=devet_simulate_attack，golden=DETECTED=YES+实际 fault），selfcheck 46/46，qwen3.8-max 实测 **pass@1 = 6/6（100%）**——每题 6.5–10s、0 工具错误、实际调用 devet_build_scenario+devet_simulate_attack 并正确报告 fault_type 与 blame 首节点。测试：devet mirror 4 项（happy path/tamper 归因/State 防御拷贝/503 降级）、agent devet_hook 5 项（authentic/检出伪造/后端宕/未配置禁用/task E2E 工具计数）、serve devet 端点 3 项；`go vet`+`go test ./...` 全绿。`competition/02、03` 已同步更新。诚实边界：镜像证明是**结果承诺级**验签（sha256 绑定 + 授权密封 + 7 项递归检查，与 DeVET 演示后端同口径），非 TLS notary 级网络证明；后端宕机时降级为「未验证」标注而非拒绝结果；真实子代理委派链的逐轮镜像成本未做压测（benchmark 端点已有 0.024ms 单次验证基线）。

**P4-2 内置技能与学习闭环**
- 目标：内置 20 个高质量技能（git/代码审查/测试修复/文档生成/翻译等）；`background_review` 的建议自动走 `remember`；技能安全审计（AST/危险模式扫描，补齐 comparison.md 的 ❌）。
- 验收：技能索引 20 条；审计器对含危险命令的技能文件报出并拒绝加载。
- 状态（2026-08-13）：✅ 已交付——①新增仓库内置 `skills/` 20 个高质量技能（git-workflow/code-review/fix-tests/docs-generation/translation/refactoring/debugging/security-audit/performance-tuning/database-design/api-design/docker-deploy/scripting/regex/data-processing/network-diagnosis/build-release/log-analysis/code-search/context-hygiene），每个含 frontmatter（name/description/triggers/read_only）+ 可直接执行的中文规范正文；boot 启动必发现 `skills/` + `<bounty-data>/skills` + 配置路径，并接入 `disabled_skills` 过滤（此前该配置字段未接线）。②技能安全审计 `skill/audit.go`：8 条规则（递归删除/管道进 shell/提权/历史改写/fork-bomb/base64 解码执行/凭据外泄/下载即执行，大小写不敏感子串匹配+命中片段回显），`Store.Discover` 对命中技能**拒绝加载**并记录 `Rejected`（名称/路径/Findings），boot stderr 报出。③`background_review` 建议自动走 remember：核实已接线（RunReview→persistSuggestions→memory.RememberStore.Save，注入/自复制标记拒绝），7 项既有测试覆盖。测试：skill 新增 5 项全绿——含验收锁 `TestBuiltinSkillsIndexTwenty`（技能索引=20 且全部通过审计）+ 危险样本 8 规则逐条拒绝 + 拒绝不进索引 + Disable 过滤 + 干净正文放行；`go vet`+`go test ./...` 全绿。诚实边界：审计为正文子串扫描（非 AST/语义级，可通过拆词/编码混淆绕过；技能正文目前不自动执行，风险面是提示词层面）；20 个技能的「触发即注入正文」加载器未做（当前为索引级技能，与既有 comparison 口径一致）。

**P4-3 无头与 CI 形态**
- 目标：`run --json` 输出规范化事件流（已有 event 包）；一个 GitHub Action 示例（PR 自动 code review + 跑测试）；后台长命令（bash 超 60s 自动转后台，`bash_output` 轮询）。
- 验收：示例 Action 在仓库 PR 上真实跑通一次。
- 状态（2026-08-13）：✅ 已交付——①后台长命令：bash 请求 timeout>60000ms 或 run_in_background=true 时后台化（默认 120s 保持同步不破坏既有行为），新增 `bash_output` 轮询（status/exit_code/最新输出尾部，GBK 解码）与 `bash_kill` 终止（Job Object/进程树全杀），进程内任务上限 16 防 fork 失控，后台命令同样过权限预检+沙箱；5 项测试覆盖（轮询到 done、超 60s 自动转后台即时返回、未知 job 报错、kill 生效、无 store 时同步回退）。②CI：`.github/workflows/ci.yml`（push+\u0026PR：go build/vet/gofmt/test+coverage、Eval selfcheck 离线 30 题、DeVET pytest）+ `pr-review.yml`（PR 自动审查示例：跑测试→构建 bounty→`bounty run` 用模型审查 PR diff 并上传 artifact；未配置 secrets.BOUNTY_API_KEY 时优雅跳过）。③已在仓库 PR #1 上真实跑通（go 全绿 + selfcheck 30/30 + devet pytest 30/30）。诚实边界：PR 模型审查需要配置 secrets.BOUNTY_API_KEY（本次验证为跳过路径，测试部分真实跑通）；后台任务仅进程内生命周期，bounty 退出即结束。

**P4-4 多模态输入（S10）**
- 目标：`provider.Message` 升级 content blocks（text+image base64），三 provider 各自映射；TUI 支持粘贴图片路径。
- 验收：截图报错 Eval 3 题通过。
- 状态（2026-08-13）：✅ 已交付——①`provider.Message` 增加 `Parts []ContentPart`（text/image），`NewUserMessage` 与 `LoadImageFile`（mime 白名单 png/jpeg/gif/webp、单图≤10MiB、base64）；文本消息保持原字符串形态（零破坏）。②四 provider 全部映射：OpenAI 兼容/native（text + image_url data URL）、Anthropic（text + image/source.base64）、Ollama（经 OpenAI 兼容客户端继承）；unit 测试锁定两端线上格式。③TUI 粘贴图片路径：输入里存在的图片文件路径（≤4 张，含带空格引号路径）自动识别并转为多模态消息，UI 回显 🖼 行；`bounty run` 支持 `--image` 重复参数。④Eval 新增 G 类 3 题（截图报错：Go 编译错误/测试失败/Python 异常栈，PIL 生成 fixture），runner 传 --image、judge 按关键点、selfcheck 49/49；qwen/qwen3.8-max 实测 3/3 通过（模型逐字读出报错并给出文件:行号）。诚实边界：G 类需多模态模型（DeepSeek 无视觉，跑 G 会失败属预期）；图片不进 SQLite 会话持久化与压缩摘要（重新加载会话后图片消息丢失，TUI 回显仍保留）；token 统计不含图片字节（按 API 返回 usage 计）。

---

## 5. 质量护栏（长期机制）

- **TDD**：每个 P 项先写测试后实现（延续 `_test.go` 先例）。
- **CI 门禁**：`go vet` + `gofmt -l` + `go test ./...` + Eval smoke 10 题。
- **性能声明可复现**：延续 `docs/benchmarks-2026-08-07.md` 的做法，新数字必须附命令与预期输出。
- **Eval 周报**：每周全量 30 题 × 各模型，回归曲线入 `docs/eval/`，倒退即拦截发布。
- **安全红线不变**：权限门/沙箱/泄露扫描改动必须过 `sandbox_test`、`gate_test`、`leak_test`；禁止绕过批准默认放行。
- **上下文纪律**：新功能不得绕过 token 预算与缓存形状追踪。

---

## 6. 定位与量化目标

- **8 周量化目标**：Eval pass@1 达基线 1.5–2 倍且 ≥50%；token/任务 ↓40%；死循环率 <2%；工具调用失败率 <5%；DeVET 攻防检测率保持 100%。
- **定位语**："DeepSeek/Qwen 底座 + Claude Code 形态 + DeVET 可验证多代理安全"。
- **答辩/论文三证据链**：①Eval 曲线（工程能力真实增长）②DeVET 归因（独有安全能力）③沙箱/权限测试（防护真实性）。

## 7. 风险与依赖

| 风险 | 影响 | 对策 |
|------|------|------|
| DeepSeek/Qwen 工具调用波动 | S6/P2-2 效果打折 | JSON 修复+重试兜底；Eval 按模型分赛道记录 |
| token-plan 政策/限流 | Eval 跑分成本 | 30 题 × 2 模型预留预算；优先跑便宜模型 |
| 单人时间不足 | 阶段滑移 | 按阶段顺序砍 P3-5/P4-4 等低杠杆项，不砍阶段 0/1 |
| Windows 生态差异 | 部分 Claude Code 能力无法对齐（如 macOS seatbelt） | 用 Job Object/Docker 等价替代，文档说明差异 |
| 公开仓库泄露风险 | 路线图含内部策略 | 本文件不含任何密钥/路径；发布前复查 |

---

## 附录 A：工作项速查（ID → 文件落点）

| ID | 主要改动文件 |
|----|--------------|
| P1-1 | `internal/agent/compact.go`（真摘要）、`internal/boot/boot.go`（ContextWindow 传递） |
| P1-2 | `internal/memory/`（检索）、`internal/tool/builtin/remember.go`、`internal/boot/boot.go`（注入） |
| P1-3 | `internal/tool/builtin/code_index.go`（索引复用）、`internal/boot/boot.go`（每轮注入） |
| P1-4 | `internal/agent/agent.go`（分工具预算）、各 builtin 工具 |
| P1-5 | `internal/tool/builtin/todo_write.go`（落库）、`internal/store/sqlite.go` |
| P2-1 | `internal/tool/builtin/edit_file.go`、`write_file.go` |
| P2-2 | `internal/provider/`（tool_choice、JSON 修复器）、`internal/agent/agent.go`（回喂重试） |
| P2-4 | `internal/tool/builtin/bash.go` |
| P3-1 | `internal/mcp/client.go`（SSE）、`internal/config/`（作用域配置） |
| P3-2 | `internal/sandbox/`（Job Object）、`internal/guardian/` |
| P3-3 | `internal/checkpoint/`（git 影子仓库） |
| P3-4 | `internal/agent/task.go`（角色/上下文/摘要） |
| P3-5 | `internal/cli/tui.go`、`cmd/bounty/main.go`（slash 命令） |
| P4-1 | `internal/devet/`、`internal/agent/task.go`（自动验证）、`internal/serve/`（可视化） |
| P4-3 | `internal/tool/builtin/bash_background.go`、`cmd/bounty/main.go`（run --json）、`.github/workflows/` |
| P4-4 | `internal/provider/provider.go`（content blocks）、三 provider 实现 |

## 附录 B：Eval 任务集（阶段 0 已交付，2026-08-13）

```
scripts/eval/
  tasks.json        # 30 题定义：A 仓库理解 10 + B 多文件改动 10 + C 修 bug 10
  fixtures/         # 三个构造仓库 go-todo / py-stats / ts-util（含注入 bug 与缺实现）
  solutions/        # 每题参考解法（仅供 selfcheck）
  config/bounty.toml
  runner.py         # 复制 fixture -> workdir，调 bounty run --json，采集步数/token/工具失败
  judge.py          # 判定器
  report.py         # markdown 报告 + docs/eval/history.csv
  selfcheck.py      # 离线自检：pristine 必失败 / solution 必通过 / golden 关键点自洽
  work/  bin/       # 运行产物（git 忽略）
```

**判定规则**：A 类=关键点覆盖；B 类=测试绿+禁改文件未动；C 类=测试绿+diff 行数 ≤ 预算。每题超 max_steps 或死循环标记=失败。
**跑分**：`python scripts/eval/runner.py --models qwen/qwen3.8-max` → `python scripts/eval/judge.py --run scripts/eval/work/<run_id>` → `python scripts/eval/report.py --run scripts/eval/work/<run_id>`。
