# P7-4 验收补课汇总（2026-08-13）

> 对应 roadmap 7.0 盘点表中 4 项待补验收项。全部证据文件落库、命令可复现。

## 1. P3-1：连 3 个真实 MCP server ✅

**交付**：
- `scripts/mcp/realcheck/main.go`：Go 验证 harness（import `internal/mcp`），连接 3 个真实 server、列出工具、各调一个工具；
- `scripts/mcp/realcheck_config.json`：3 server 配置（stdio x2 + SSE x1）；
- `scripts/mcp/sse_math_server.py`：官方 Python SDK（FastMCP 1.27）SSE server（add / calc_fib）；
- `scripts/mcp/npm/`：官方 reference server 本地依赖（filesystem / memory，@modelcontextprotocol/server-*）；
- `scripts/mcp/run_realcheck.py`：一键复现（自启 SSE server → realcheck → 存转写）。

**证据**：`docs/eval/p7-4-mcp-transcript.txt`

| server | 传输 | 实现 | 工具调用 | 结果 |
|---|---|---|---|---|
| filesystem | stdio | 官方 Node reference server | list_directory | 列出 C:\\bounty-eval\\work 3 个 run 目录 |
| memory | stdio | 官方 Node reference server | create_entities → read_graph | entity 写入并读回 |
| math-sse | SSE | 官方 Python SDK FastMCP | add / calc_fib | 42 / 55 |

复现：`python scripts/mcp/run_realcheck.py`

**E1/E2 复用真实 server**：eval fixture `mcp-math/mcp_server.py` 由手写 JSON-RPC 假 server 升级为官方 SDK（FastMCP）实现（同一 add/calc_fib 工具集）。run `20260813-mcp-real`（`--task-ids E1,E2`）：**E1/E2 均 PASS**（必需工具 `mcp__math__` 实际调用、工具失败率 0%）。报告 `docs/eval/20260813-mcp-real-report.md`。

## 2. P3-4：子代理摘要 token before/after ✅

**交付**：`internal/agent/task_token_ratio_test.go`
- `TestSubagentSummaryTokenRatioUnit`（离线单测）：ASCII 长输出 31,504→1,588 tok（**5.0%**）；CJK 长输出 33,079→2,788 tok（**8.4%**）。均 ≤50% 线。
- `TestSubagentSummaryTokenRatioRealModel`（真实模型，`BOUNTY_REAL_TOKEN_TEST=1` 门控）：qwen/qwen3.8-max 派 explore 子代理输出超长报告，原始输出 **20,883 tok**（13,166 runes）→ 结构化摘要 **3,792 tok**，**占比 18.2%**（下降 81.8%）。

**证据**：`docs/eval/p7-4-subagent-token.txt`

复现：
- 单测：`go test ./internal/agent/ -run TestSubagentSummaryTokenRatioUnit -v`
- 真实模型：`BOUNTY_REAL_TOKEN_TEST=1 go test ./internal/agent/ -run TestSubagentSummaryTokenRatioRealModel -v -timeout 300s`

## 3. P3-5：TUI 真实 TTY 录屏三场景 ✅

**交付**：`scripts/cli/tui_record.py`（pywinpty 驱动 `bounty chat`，winpty 真实 TTY；`python -X utf8` 规避 GBK 编码问题）。36x110 真实控制台、逐键注入、ANSI 全程转写 257KB。

**证据**：`docs/eval/p7-4-tui/transcript.log`

| 场景 | 操作 | 证据点 |
|---|---|---|
| S1 导航 | ↑↓ x3、PgUp/PgDn/Home/End | 每次按键产生重绘帧（独立实验：5x↑ → 1,292B 新渲染输出）；TUI 持续 tick 渲染（时间戳逐秒更新） |
| S2 折叠 | e（展开最近工具调用） | glob 面板展开：`┌ glob 🔎 │ args: {...} │ 文件列表 │ └ ✓` |
| S2 折叠 | E（全部展开/折叠） | 全部工具面板进入展开态（transcript 后续帧面板均为展开渲染） |
| S3 diff | e（展开 edit_file 面板） | `┌ edit_file 🔧 │ args: {"file_path": ...} │ diff: │ - `internal/format` — 列表格式化（FormatList）` |

复现：`python -X utf8 scripts/cli/tui_record.py`（需要 qwen key；~6 分钟）

## 4. P1-3：A 类步数回归 ✅（未达 30% 线，如实记录）

| run | A 类平均步数 | vs 基线 | 说明 |
|---|---|---|---|
| P5 基线 165619 | 3.3 | - | 基线 |
| P7-2 215848 | 2.4 | **↓27.3%** | glob-first 策略轮 |
| P7-3 最终 222946 | 2.5 | **↓24.2%** | 系统提示瘦身轮（方差） |

结论：A 类步数下降稳定在 24–27%，**未达 30% 验收线**；P7-3 轮略回升属模型方差（输入 token 下降 24.4% 以步数为代价交换）。8 周目标中该项继续列为未达项，后续杠杆：A 类任务注入更精确的定位提示（如 repomap 按题目命中率重排）。

## 5. 验收状态

7.0 盘点表更新：
- P3-1 连 3 个真实 MCP server：⏳待补 → ✅ 闭环（本文件 §1）
- P3-4 子代理输出 token ↓50%：⏳待补 → ✅ 闭环（本文件 §2，18.2%）
- P3-5 TUI 录屏：⏳待补 → ✅ 闭环（本文件 §3）
- P1-3 A 类步数 ↓30%：⏳未达 → ⏳未达（24.2–27.3%，本文件 §4）
