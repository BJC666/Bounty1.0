# 论文 → Bounty 融入建议

> 分析日期：2026-08-05 · 输入：`C:\Users\21671\Desktop\大模型论文`（17 篇 CCS '25 / KDD '25 / ASIA CCS '26 论文 + VET 复现工程）
> 结论先行：**4 项可直接落地（低成本、运行时即可生效），3 项需设计（中高成本），其余为模型训练/评估方向不适合融入 Go 单二进制**。

---

## 一、可直接融入（建议按顺序实施）

### 1. BIPIA — 不可信内容边界标记（最高性价比）
**论文要点**：间接注入攻击成功的关键是 LLM 分不清"数据"和"指令"；白盒防御用特殊标记 `<data></data>` 包裹外部内容 + 对抗微调，可将 ASR 降到接近 0。
**Bounty 现状**：`internal/memory/injection.go` 的 `ScanInjection` **只在 `remember` 工具调用**；`web_fetch.go` 返回的网页原文**无任何标记/扫描**，直接进对话上下文——这正是"网页内容 → 任务上下文"的注入链路。
**改动**：
- `web_fetch.go`：返回内容前用 `<data url="...">…</data>` 包裹（纯文本包裹，零依赖）；
- `internal/memory/loader.go`：4 级记忆加载结果同样包裹/扫描；
- `ScanInjection` 在 agent 每轮把外部数据注入上下文前调用（`PostToolUse` hook 处）。
**成本**：约 30-50 行，可单测。

### 2. RAGworm / DonkeyRail — 注入扫描器加强 + 自复制蠕虫检测
**论文要点**：自复制 prompt 可经 RAG 检索链式传播；DonkeyRail 用轻量相似度检测（TPR=1.0，FPR=0.017，延迟 <40ms）。
**Bounty 现状**：`InjectionPatterns` 只有 17 条基础模式，无"复制并转发"类特征、无传播链检测。
**改动**：
- `InjectionPatterns` 扩充：`"ignore all instructions"`、`"disregard"`、`"system instruction"`、伪装成任务指引的 `"task-aligned"` 模式（Mind the Web 类型：`"to complete this task, visit …"`）；
- 新增 `ScanSelfReplication(content)`：检测"复制我/转发/粘贴到新对话/把这段话发出去"类传播指令，在 `remember` 写入和 web 内容注入前拦截；
- 可选：对记忆库做两两相似度（SimHash）检测，发现批量注入记忆即报警。
**成本**：约 40-60 行 + 测试。

### 3. CoT Leakage — 推理过程敏感信息泄露检测
**论文要点**：CoT 推理可能"无意泄露"——最终答案拒绝了请求，但推理过程包含可操作的有害内容。
**Bounty 现状**：`reasoning` 事件经 SSE 流式输出到 Web 控制台（`cmd/bounty/main.go` `serveSSE`、`internal/serve/dashboard.go`），无泄露检测。
**改动**：新增 `internal/memory/leak.go`（或并入 injection 包）——对 reasoning/text 流做轻量正则扫描（`sk-[A-Za-z0-9]`、`AKIA…`、`-----BEGIN…`、`~/.ssh`、绝对路径、`password=` 等），命中即打码或转为 `[redacted]`，并记录安全事件。
**成本**：约 30 行 + 模式表，纯本地正则零延迟。

### 4. Mind the Web / AgentSentinel — 动作审计日志（任务上下文关联）
**论文要点**：AgentSentinel 的审计核心是"把当前任务上下文与系统 trace 关联"；Mind the Web 建议 LLM-as-Judge 隔离验证（judge 只看任务+动作，不看网页内容）。
**Bounty 现状**：已有 `PreToolUse`/`PostToolUse` hook 事件流（`internal/hook`），但无结构化"敏感动作审计日志"，也没有"动作 vs 原始任务"的一致性检查。
**改动**（分两步）：
- 第一步（低成本）：`agent.go` 每步把 `(任务摘要, 工具名, 参数摘要, 结果摘要)` 追加到会话审计日志（已有事件流，只是落库/导出），供事后审查——这就是 AgentSentinel 的审计 trace；
- 第二步（中成本）：对 `bash`/`write_file`/`browser` 高危动作，在 yolo/auto 模式下用 LLM-as-Judge（复用 DeVET 后端或本地小模型）做"动作 vs 任务"一致性判定，结果缓存（AgentSentinel 的 security query cache 思路）避免每步都调模型。

---

## 二、需设计的融入（中高成本）

### 5. AgentSentinel — 系统级敏感操作 trace（进程/文件/网络）
论文在 OS 层拦截并审计所有敏感操作。Bounty 是 Go 单二进制，OS 级拦截不现实，但可做**近似**：bash 工具执行时包装命令（记录命令哈希 + 退出码 + 时间戳到不可变审计日志），配合第一步的审计 trace 形成"决策-执行"对照链。成本中等，价值在事后取证。

### 6. LLM-Eraser — 记忆遗忘/删除
论文方向是模型级机器遗忘；Bounty 的 `skill.Curator` 已有记忆生命周期管理（active→inactive→archived），可扩展"删除/遗忘"策略：按时间、来源（是否来自不可信网页）、或用户指令清除记忆条目，防止被污染的 RAG 记忆持续影响后续会话。约 60-100 行。

### 7. FlippedRAG / GASLITE — RAG 检索攻击防御
论文展示攻击者通过网页 SEO/文档投毒操纵 RAG 检索结果。Bounty 的记忆/技能加载是 RAG 类似物：给每条记忆/技能记录**来源与信任级别**（本地用户文件=高，网页抓取=低），低信任内容注入上下文时带 `<data>` 标记 + 来源标注，并降低其在系统提示中的权重。需改 `memory.Doc` 结构。

---

## 三、不适合直接融入（记录备查）

| 论文 | 方向 | 为何不融入 |
|------|------|-----------|
| SecAlign | DPO 偏好优化训练 | 模型训练方法，非运行时；可借鉴到系统提示（偏好安全输出） |
| Knowledge-to-Jailbreak / YouthSafe / MediRed / ALSA | 越狱评估/青少年安全/医疗隐私/遗忘数据 | 训练/评估/领域数据集方向 |
| VFLAIR-LLM / TrustGLM | 分割学习/图 LLM 鲁棒性 | 训练框架与图学习评估 |
| Biased-Roots | 蒸馏供应链投毒 | 供应链风险，需模型溯源，超出 agent 运行时 |
| VET（vet-repro） | 可验证执行轨迹 | **已是 Bounty 的 DeVET 项目来源**；AID/Web Proof/TEE 依赖 Rust/TLSNotary/Linux，不适合进 Go 单二进制，但可对照 `internal/devet/devet.go` 的 5 个工具检查组合验证完整性 |

---

## 四、实施状态

- ✅ **已完成（2026-08-05）**：
  - 第 1 项 BIPIA `<data>` 边界标记 + 注入扫描加强（9 个单测）——HANDOFF.md 第十五节
  - 第 3 项 CoT 推理泄露脱敏（fanout 层 RedactSensitive，6 个单测）——HANDOFF.md 第十六节
  - **复现套件**：`repro/repro_test.go`（BIPIA/RAGworm/Mind the Web/CoT Leakage 共 7 用例）+ `docs/repro-papers-2026-08-05.md`
- ⏳ **待实施**：
  1. **动作审计日志**（1 天）— 为后续 LLM-as-Judge 和取证打基础（落地后补复现用例）
  2. **RAG 来源信任级别**（1-2 天）— 结构性防御，需改记忆结构

> 注：第 3 步的 LLM-as-Judge 一致性检查与 Bounty 现有 DeVET 独立验证思想一致，可复用 `devet_*` 工具链。