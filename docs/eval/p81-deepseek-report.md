# Bounty Eval 基线报告（run deepseek-v4-pro 基线（P8-1））

- 生成时间：2026-08-15 07:31
- 任务集：`scripts/eval/tasks.json`（A 仓库理解 10 + B 多文件改动 10 + C 修 bug 10 + D 记忆 3 + E 生态 7 + F DeVET 攻防 7 + G 多模态 3）
- 判定规则：A=关键点命中；B=测试命令通过+禁改文件未动；C=测试通过+diff 行数≤预算；D=关键点命中；E=关键点命中+必需工具前缀实际调用；全部限 max_steps=50

## 总览

| 模型 | pass@1 | A | B | C | D | E | F | G | 平均步数 | 平均输入 tok | 平均输出 tok | 工具调用失败率 | 验证失败率 | 自愈率 | 超时数 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| deepseek/deepseek-v4-pro | 94.0% | 100.0% | 100.0% | 100.0% | 100.0% | 100.0% | 100.0% | 0.0% | 3.2 | 12352 | 419 | 1.5% | 0.0% | 100.0% | 0 |

## 模型 deepseek/deepseek-v4-pro

| 任务 | 类别 | 判定 | 步数 | 输入 tok | 输出 tok | 工具失败(t/v) | 用时(s) | 原因/备注 |
|---|---|---|---|---|---|---|---|---|
| A1 列表渲染函数 | 仓库理解 | 通过 | 2 | 7396 | 188 | 0/0 | 4.5 | 关键点全部命中 |
| A10 daysBetween 实现 | 仓库理解 | 通过 | 2 | 6930 | 233 | 0/0 | 4.4 | 关键点全部命中 |
| A2 Todo 结构体定义 | 仓库理解 | 通过 | 2 | 7559 | 177 | 0/0 | 4.4 | 关键点全部命中 |
| A3 标题长度上限 | 仓库理解 | 通过 | 2 | 7367 | 206 | 0/0 | 4.1 | 关键点全部命中 |
| A4 done 子命令调用链 | 仓库理解 | 通过 | 2 | 8029 | 1241 | 0/0 | 12.5 | 关键点全部命中 |
| A5 median 实现位置 | 仓库理解 | 通过 | 2 | 7433 | 330 | 0/0 | 5.5 | 关键点全部命中 |
| A6 read_csv 返回值 | 仓库理解 | 通过 | 2 | 7252 | 434 | 0/0 | 6.0 | 关键点全部命中 |
| A7 analyze 调用链 | 仓库理解 | 通过 | 2 | 7810 | 302 | 0/0 | 5.3 | 关键点全部命中 |
| A8 truncate 实现位置 | 仓库理解 | 通过 | 2 | 6969 | 247 | 0/0 | 4.9 | 关键点全部命中 |
| A9 formatPercent 行为 | 仓库理解 | 通过 | 2 | 6927 | 299 | 0/0 | 5.1 | 关键点全部命中 |
| B1 实现 search 搜索 | 多文件改动 | 通过 | 4 | 16844 | 514 | 0/0 | 10.2 | 测试通过，diff=19 行 |
| B10 实现 relativeTime 相对时间 | 多文件改动 | 通过 | 5 | 20439 | 666 | 0/0 | 10.9 | 测试通过，diff=12 行 |
| B2 实现 priority 优先级映射 | 多文件改动 | 通过 | 4 | 15897 | 433 | 0/0 | 9.4 | 测试通过，diff=15 行 |
| B3 实现 export 导出 JSON | 多文件改动 | 通过 | 4 | 17311 | 715 | 0/0 | 12.1 | 测试通过，diff=12 行 |
| B4 实现 undo 栈 | 多文件改动 | 通过 | 4 | 15815 | 517 | 0/0 | 8.4 | 测试通过，diff=28 行 |
| B5 实现 variance/stddev | 多文件改动 | 通过 | 4 | 17106 | 699 | 0/0 | 11.6 | 测试通过，diff=16 行 |
| B6 实现 group_by 分组 | 多文件改动 | 通过 | 4 | 17367 | 713 | 0/0 | 10.7 | 测试通过，diff=12 行 |
| B7 实现 render_json 渲染 | 多文件改动 | 通过 | 4 | 16045 | 598 | 0/0 | 10.4 | 测试通过，diff=11 行 |
| B8 实现 slugify | 多文件改动 | 通过 | 4 | 17510 | 1076 | 0/0 | 13.4 | 测试通过，diff=6 行 |
| B9 实现 median 中位数 | 多文件改动 | 通过 | 7 | 36261 | 1760 | 2/0 | 21.3 | 测试通过，diff=11 行 |
| C1 修复 Done 差一错误 | 修 bug | 通过 | 4 | 15949 | 398 | 0/0 | 8.3 | 测试通过，diff=2 行 |
| C10 修复 clamp 下界 | 修 bug | 通过 | 4 | 14766 | 367 | 0/0 | 7.1 | 测试通过，diff=2 行 |
| C2 修复 Pending 逻辑反转 | 修 bug | 通过 | 5 | 21951 | 782 | 0/0 | 11.9 | 测试通过，diff=2 行 |
| C3 修复标题超长校验 | 修 bug | 通过 | 4 | 15886 | 444 | 0/0 | 8.7 | 测试通过，diff=2 行 |
| C4 修复列表编号起点 | 修 bug | 通过 | 4 | 16004 | 426 | 0/0 | 9.8 | 测试通过，diff=2 行 |
| C5 修复 median 偶数长度 | 修 bug | 通过 | 4 | 16418 | 541 | 0/0 | 9.3 | 测试通过，diff=2 行 |
| C6 修复 quantile 位置公式 | 修 bug | 通过 | 4 | 16804 | 750 | 0/0 | 10.6 | 测试通过，diff=2 行 |
| C7 修复 read_csv 丢首行 | 修 bug | 通过 | 4 | 15241 | 311 | 0/0 | 8.0 | 测试通过，diff=2 行 |
| C8 修复 CLI mode 分支 | 修 bug | 通过 | 4 | 17099 | 495 | 0/0 | 11.5 | 测试通过，diff=2 行 |
| C9 修复 truncate 省略号 | 修 bug | 通过 | 4 | 15759 | 647 | 0/0 | 9.5 | 测试通过，diff=2 行 |
| D1 记忆闭环：保存并复述偏好 | 记忆 | 通过 | 3 | 11176 | 399 | 0/0 | 6.7 | 关键点全部命中 |
| D2 记忆闭环：保存两条并检索相关项 | 记忆 | 通过 | 3 | 11385 | 474 | 0/0 | 6.5 | 关键点全部命中 |
| D3 记忆闭环：空查询列出最近记忆 | 记忆 | 通过 | 3 | 11029 | 285 | 0/0 | 6.3 | 关键点全部命中 |
| E1 MCP 加法工具 | 生态（MCP/子代理） | 通过 | 2 | 6779 | 163 | 0/0 | 4.8 | 关键点全部命中；必需工具 mcp__math__ 已实际调用 |
| E2 MCP 斐波那契工具 | 生态（MCP/子代理） | 通过 | 2 | 6649 | 92 | 0/0 | 4.8 | 关键点全部命中；必需工具 mcp__math__ 已实际调用 |
| E3 子代理调研：main.go 函数数量 | 生态（MCP/子代理） | 通过 | 4 | 12989 | 498 | 0/0 | 6.7 | 关键点全部命中；必需工具 task 已实际调用 |
| E4 子代理调研：stats 目录文件数 | 生态（MCP/子代理） | 通过 | 4 | 11978 | 317 | 0/0 | 6.7 | 关键点全部命中；必需工具 task 已实际调用 |
| E5 子代理调研：strings.ts 导出数量 | 生态（MCP/子代理） | 通过 | 4 | 11334 | 398 | 0/0 | 9.1 | 关键点全部命中；必需工具 task 已实际调用 |
| E6 子代理调研：core.py 函数数量 | 生态（MCP/子代理） | 通过 | 4 | 12015 | 206 | 0/0 | 6.3 | 关键点全部命中；必需工具 task 已实际调用 |
| E7 只读子代理：README 首行 | 生态（MCP/子代理） | 通过 | 4 | 12284 | 441 | 0/0 | 8.6 | 关键点全部命中；必需工具 read_only_task 已实际调用 |
| F1 DeVET 攻防：委托替换 A1 | DeVET 攻防 | 通过 | 3 | 11474 | 190 | 0/0 | 5.1 | 关键点全部命中；必需工具 devet_simulate_attack 已实际调用 |
| F2 DeVET 攻防：子结果伪造 A2 | DeVET 攻防 | 通过 | 3 | 11506 | 190 | 0/0 | 5.6 | 关键点全部命中；必需工具 devet_simulate_attack 已实际调用 |
| F3 DeVET 攻防：API 超限 A4 | DeVET 攻防 | 通过 | 3 | 11290 | 128 | 0/0 | 4.3 | 关键点全部命中；必需工具 devet_simulate_attack 已实际调用 |
| F4 DeVET 攻防：Grant 篡改 A7 | DeVET 攻防 | 通过 | 3 | 11386 | 163 | 0/0 | 4.4 | 关键点全部命中；必需工具 devet_simulate_attack 已实际调用 |
| F5 DeVET 攻防：跨 Host 共谋 A8 | DeVET 攻防 | 通过 | 3 | 11445 | 164 | 0/0 | 5.3 | 关键点全部命中；必需工具 devet_simulate_attack 已实际调用 |
| F6 DeVET 攻防：重放攻击 A10 | DeVET 攻防 | 通过 | 3 | 11350 | 161 | 0/0 | 5.9 | 关键点全部命中；必需工具 devet_simulate_attack 已实际调用 |
| F7 DeVET 攻防：真实性证明缺失 A11 | DeVET 攻防 | 通过 | 3 | 11382 | 170 | 0/0 | 5.8 | 关键点全部命中；必需工具 devet_simulate_attack 已实际调用 |
| G1 截图报错-Go 编译错误 | 多模态（截图报错） | 失败 | 1 | 0 | 0 | 0/0 | 0.8 | bounty 进程异常退出 exit=1: Error: step 0: [FatalError] {"error":{"message":"Failed to |
| G2 截图报错-Go 测试失败 | 多模态（截图报错） | 失败 | 1 | 0 | 0 | 0/0 | 0.8 | bounty 进程异常退出 exit=1: Error: step 0: [FatalError] {"error":{"message":"Failed to |
| G3 截图报错-Python 异常栈 | 多模态（截图报错） | 失败 | 1 | 0 | 0 | 0/0 | 0.8 | bounty 进程异常退出 exit=1: Error: step 0: [FatalError] {"error":{"message":"Failed to |

## 失败明细

### deepseek/deepseek-v4-pro / G1 截图报错-Go 编译错误

- 原因：bounty 进程异常退出 exit=1: Error: step 0: [FatalError] {"error":{"message":"Failed to deserialize the JSON body into the target type: messages[1]: unknown variant `image_url`, expected `text` at line 1 column 22547","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}

### deepseek/deepseek-v4-pro / G2 截图报错-Go 测试失败

- 原因：bounty 进程异常退出 exit=1: Error: step 0: [FatalError] {"error":{"message":"Failed to deserialize the JSON body into the target type: messages[1]: unknown variant `image_url`, expected `text` at line 1 column 23597","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}

### deepseek/deepseek-v4-pro / G3 截图报错-Python 异常栈

- 原因：bounty 进程异常退出 exit=1: Error: step 0: [FatalError] {"error":{"message":"Failed to deserialize the JSON body into the target type: messages[1]: unknown variant `image_url`, expected `text` at line 1 column 33220","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}
