# Bounty 1.0 安全审查报告

> 审查日期：2026-08-05 · 范围：全仓库（Go 1.25，cmd/bounty + internal/*）
> 方法：静态代码审计（对照 Go 后端安全最佳实践），本轮仅发现与建议，未改动代码。
> 前置状态：P0 修复（Ask 决策、BOUNTY_AUTH_TOKEN、SSRF、路径穿越、guardianGate）已在上轮完成。

## 执行摘要

整体安全基线良好：SQL 全部参数化、密钥仅存环境变量且无日志泄露、web_fetch 已具备 SSRF 防护、注入扫描已接线、默认 deny 命令黑名单生效。
本轮发现 **2 项高危、4 项中危、5 项低危**。高危集中在两处：**HTTP 通道认证缺失**（攻击者可远程驱动 agent 读写任意文件）与**文件系统保护失效**（ForbidRead 未接线、ForbidWrite 模式对绝对路径不匹配）。

---

## 高危

### H1. HTTP 通道无认证，可远程驱动 agent 执行工具（含任意文件读写）
- 位置：`internal/channel/httpapi/httpapi.go:45`（`/api/message`）、`internal/channel/httpapi/httpapi.go:75`（`/api/status`）；`internal/channel/gateway.go:56`（`/webhook/`）、`internal/channel/gateway.go:98`（`/chat`）、`internal/channel/gateway.go:79`（`/events`）
- 影响：`serve` 命令启动的 Gateway（:8080）与 HTTP API 通道（:9090）全部端点**无任何认证**。结合默认 `auto` posture 下非 bash 工具直接放行（`internal/permission/gate.go:97` 兜底 `return Allow`），局域网/公网攻击者可以：
  1. `read_file`/`grep`/`glob` 读取本机任意文件（`internal/tool/builtin/read_file.go:31` 直接 `os.ReadFile`）；
  2. `write_file` 写入任意路径（配合 H2 的写保护失效，可写启动项等实现持久化）；
  3. 无限驱动 agent 消耗 LLM token。
- 建议：为 Gateway 与 HTTPAPI 通道的所有端点复用 `BOUNTY_AUTH_TOKEN` 校验（与 `cmd/bounty/main.go:315` 的 `authMiddleware` 一致）；HTTP API 通道默认关闭，需显式配置才启用；文档补充"通道默认监听 127.0.0.1"。

### H2. 文件系统保护失效：ForbidRead 未接线 + ForbidWrite 模式不匹配绝对路径
- 位置：`internal/permission/gate.go:102-116`（`isForbidWrite` 用 `filepath.Match`）；`internal/config/defaults.go:52-59`（默认 `ForbidWrite: ["Windows/*","Program Files/*","System32/*","~/.ssh/*",...]`）
- 影响：
  1. **ForbidRead 完全未使用**：`Sandbox.ForbidRead` 仅存在于配置结构（`internal/config/config.go:45`），没有任何工具执行路径检查它，`read_file`/`grep`/`glob`/`code_index` 可读任意文件。
  2. **ForbidWrite 匹配失效**：`filepath.Match("Windows/*", "C:\Windows\System32\config\SAM")` 与 `filepath.Match("~/.ssh/*", "C:\Users\x\.ssh\id_rsa")` 均不匹配——默认写保护对绝对路径（尤其 Windows）基本无效；`~` 也不展开。
- 建议：统一做"路径策略检查"helper：将待读写路径转为绝对路径后，同时匹配用户模式与"绝对化后的模式"（把 `~` 展开、相对模式转绝对）；在 `read_file`/`grep`/`glob`/`code_index` 的 Execute 前调用 ForbidRead 检查；为 write/edit 复用同一 helper。

---

## 中危

### M1. Web 控制台存储型/反射型 XSS（innerHTML 拼接）
- 位置：`internal/serve/dashboard.go:89`（session 标题拼进 innerHTML）、`internal/serve/dashboard.go:102`（agent 输出/事件文本拼进 innerHTML）；`internal/serve/chat.go:352`、`chat.go:367`（错误信息）、`chat.go:388`（DeVET 攻击列表 `a.name/a.fault/a.desc` 未转义）
- 影响：session 标题由用户创建（`/new <标题>`），agent 输出与 DeVET 后端数据均不可信。若含 `<img onerror=...>` 等 HTML，可在 dashboard 页面执行任意 JS。本地工具场景风险有限，但 agent 输出来自网页抓取/LLM，属典型"提示注入→存储型 XSS"链路。
- 建议：innerHTML 拼接处统一改用 `textContent`/`createElement`（消息渲染已用 textContent，属正确范式）；或对拼接值先做 HTML 转义。

### M2. HTTP 服务无超时与请求体上限（慢速 DoS）
- 位置：`cmd/bounty/main.go:265`（`http.ListenAndServe(":8090", mux)` 无 ReadTimeout/WriteTimeout/MaxHeaderBytes）；`internal/channel/gateway.go:118`（`http.Server{Addr, Handler}` 无超时）；`internal/channel/httpapi/httpapi.go:82`（同）；`/chat/api/send`、`/api/message`、`/webhook/` 均无 `http.MaxBytesReader`
- 影响：慢连接可长期占用连接与 goroutine；超大请求体可耗尽内存。
- 建议：统一 `http.Server` 配置（`ReadHeaderTimeout`、`ReadTimeout`、`WriteTimeout`、`MaxHeaderBytes`）；解析 body 前加 `http.MaxBytesReader`（如 1 MiB）。

### M3. 认证 token 比较非恒定时间 + query 参数泄露
- 位置：`cmd/bounty/main.go:317-321`（`reqToken != token` 普通字符串比较；`?token=` query 参数）；`internal/serve/chat.go:29-36` 重复认证同样问题
- 影响：URL query 中的 token 会进入访问日志/浏览器历史/Referer；非恒定时间比较存在理论时序侧信道。
- 建议：优先仅支持 `Authorization: Bearer` 头；用 `crypto/subtle.ConstantTimeCompare` 比较；query 参数支持可保留但需提示不用于生产。

### M4. 权限白名单名称不匹配，Allow 配置形同虚设
- 位置：`internal/permission/gate.go:42-44`（`g.allowed` 存小写工具名）+ `internal/config/defaults.go:24-27`（`Allow.Tools: ["Read","Glob","Grep","Write","Edit",...]`）
- 影响：工具实际名为 `read_file`/`write_file` 等，`"Read"` 等条目永不匹配 `allowed` 表；auto posture 下所有未匹配工具都走兜底 Allow（`gate.go:97`），白名单完全没起作用（默认全放行的行为掩盖了配置错误）。
- 建议：统一为工具真实名称（`read_file`、`write_file`、`grep`、`glob`、`web_search`、`web_fetch`、`todo_write`、`remember`、`browser`、`bash`、`task`、`fleet` 等）；或让 `NewGate` 同时规范化大小写与下划线。

---

## 低危

### L1. bash 工具超时参数无上限
- 位置：`internal/tool/builtin/bash.go:40-41`（`params.Timeout` 直接覆盖默认 120s，schema 标注 max 600000ms 但代码不校验）
- 影响：模型/攻击者可指定任意大超时，命令长时间运行占用资源。
- 建议：钳制到 `[1s, 600s]`。

### L2. Dockerfile 以 root 运行
- 位置：`Dockerfile`（无 `USER` 指令，`ENTRYPOINT ["bounty"]`）
- 影响：容器内 bash 工具以 root 执行，一旦逃逸即主机 root。
- 建议：创建非 root 用户并 `USER bounty`；`chat` 为交互命令，容器默认 CMD 建议改为 `run`/`serve` 并配健康检查。

### L3. ssh 首次连接接受任意主机密钥（TOFU）
- 位置：`internal/remote/ssh.go:31`（`StrictHostKeyChecking=accept-new`）
- 影响：首次连接无指纹校验，存在中间人风险。
- 建议：默认 `StrictHostKeyChecking=yes` 并要求配置 known_hosts，或至少文档说明 TOFU 风险。

### L4. deny 命令可被别名绕过（git push -f）
- 位置：`internal/config/defaults.go:49`（`git push --force *` 精确匹配）+ `internal/permission/gate.go:121-130`（`matchBashPattern` 仅精确/前缀匹配）
- 影响：`git push -f`、`git push --force-with-lease` 等变体不匹配 deny 模式。
- 建议：deny 模式对关键命令做规范化（统一 `-f`→`--force` 或增加 `-f` 变体）。

### L5. 其他小项
- `web_search` 域名过滤为子串匹配（`internal/tool/builtin/web_search.go`），`evil-example.com` 可匹配 `example.com` 白名单——建议按主机名边界匹配。
- `devet/launcher.go` 的 `isRunning` 使用无超时默认 client（localhost 场景低风险）。
- `store/sqlite.go:149` `LIKE ?` 拼接 `%query%`，通配符可扩大匹配范围（非注入）。
- **依赖漏洞扫描缺失**：`govulncheck` 未集成且当前环境无法安装（proxy.golang.org 不可达）；go.mod 依赖版本较新（charmbracelet v1.3.x、sqlite3 v1.14.x），建议 CI 加入 `govulncheck`。
- yolo 模式 guardian（`internal/guardian/guardian.go`）仅覆盖 bash/write/edit 且为子串匹配，yolo 下 `read_file` 读 `.env` 不拦截——属信任模式设计权衡，文档应明确。

---

## 优先级建议（修复顺序）

1. **H1**：Gateway/HTTPAPI 端点统一认证（改动小、收益最大）。
2. **H2**：路径策略 helper（ForbidRead 接线 + ForbidWrite 绝对化匹配），并补单元测试。
3. **M1**：dashboard/chat 的 innerHTML → textContent（3 处）。
4. **M2**：HTTP server 超时 + body 限制。
5. **M3/M4/L1**：认证比较、白名单名称、bash 超时钳制。

> 说明：TLS 未列为问题——本项目为本地/局域网开发工具，未配置 TLS 属于部署场景，可按需通过反代提供。

## 2026-08-13 增补：P3-2 Windows Job Object 沙箱
- 新增：bash 子进程挂 Job Object（KILL_ON_JOB_CLOSE、禁 breakaway），容器关闭连坐击杀整棵进程树；实测孤儿 ping.exe 被回收。
- 新增：Network=false 时出站预检（curl/pip/npm/git clone/Invoke-WebRequest/WebClient 等 19 类）+ 代理环境毒化；注意这是环境级阻断，非内核 WFP，绕过需直接 TCP 编程——文档已声明，可作后续增强。
- 新增：bash 命令路径预检——引号感知重定向解析，workspace+allow_write 之外写入、forbid_read/forbid_write 命中即拦截（与权限门策略同语义）。
- 已知边界：管道/命令代换中间接路径（`echo x > $(calc)` 类）无法静态覆盖；深子进程系统调用级 FS 限制未做（需要 ACL/AppContainer）。风险等级：中低。

# 2026-08-13 增补：P3-3 git 影子仓库检查点

## 设计
- 会话启动时在用户级数据目录 `~/bounty-data/checkpoints/<session>/shadow.git` 建裸仓库（位于工作区之外，不会自嵌套）；每条用户消息前做全量快照并打 `msg-<N>` 标签，web 端可一键回滚到任意消息。
- 快照通过 `GIT_DIR`/`GIT_WORK_TREE`/`GIT_INDEX_FILE` 环境变量隔离，不触碰工作区真实 `.git` 的任何状态（不写 refs、不跑工作区 hooks——bare 仓库默认无 hooks）。
- `git add` 使用 `:(exclude).git` pathspec 排除真实仓库目录；回滚删除阶段同样跳过 `.git`；BOUNTY_HOME 若指向工作区内，影子目录自动从快照与清理中排除。

## 安全属性
- 回滚不执行任何来自工作区的代码：恢复路径只有 `ls-tree`（读对象库）→ 文件系统删除/写入 → `read-tree`/`checkout-index`（写文件），不触发 hook、脚本或构建。
- `-c core.autocrlf=false`/`core.eol=lf`/`core.safecrlf=false` 固定配置，避免全局 git 配置（autocrlf 转换等）破坏快照的字节精确性。
- 回滚 API 继承 web 控制台既有认证（BOUNTY_AUTH_TOKEN 恒定时间比较），前端二次确认后才执行；恢复目标是"消息开始前"状态，语义可预期。

## 已知风险与边界
- 快照是工作区全量镜像（含 .env 等敏感文件，与 Claude Code 同类工具的本地 checkpoint 行为一致）：影子仓库只在本地用户数据目录，不推送、不随仓库分发；但若工作区本身含密钥，等于在 bounty-data 下多了一份副本，磁盘泄露面与 session store 相同。
- `git add -f` 会纳入被 .gitignore 忽略的构建产物/本地文件，换取回滚精确性；大型仓库每消息有全量扫描成本（性能换语义，已在路线图标注）。
- 回滚为文件层面，不截断会话消息历史；回滚与正在运行的工具轮存在竞态（UI 未加轮运行锁，待 P3-5 收口）。

# 2026-08-13 增补：P3-4 子代理增强（安全边界）

- 上下文注入：子代理收到的「父任务上下文」只来自父会话中同轮任务相关的用户消息（≤2KB），不跨会话、不含密钥——与父代理同信任域，不扩大数据暴露面。
- model 覆盖：子代理换模型走 boot 的 ProvFactory，密钥仍从同一 `secrets` 池读取、不落盘不打印；未配置 factory 时明确拒绝，不存在静默降级到父 provider 的歧义。
- 结构化摘要：父代理消费的仅是子代理产出的三节摘要（结论上限 1200 rune），子代理原始输出不进入父上下文，间接缩小了恶意长输出的注入面。
- 已知边界：explore/general 仅控制工具集过滤与提示词，不构成权限边界（写路径仍由父 agent 的 gate/sandbox 在工具执行层统一把关）；子代理模型覆盖依赖配置中可信 provider 定义。

# 2026-08-13 增补：P3-5 TUI 打磨 + Slash 命令（安全边界）

- 权限确认弹窗（tuiAsker）：TUI 内敏感操作决策内联呈现，数字键选择方案、Esc 一律视为拒绝、请求 ctx 超时自动清空——决策不落盘、不缓存，权限门语义不变弱。
- `/model` 切换：boot.SwitchModel 只能切到配置内已定义的 provider（裸名时走默认 provider 家族），密钥仍从 `secrets` 池读取，不打印、不落盘；未定义 provider 明确报错。
- `/export`：仅导出会话文本到显式给定路径（默认当前目录 `会话名.md`），不携带密钥（事件流中密钥本就遮蔽）；不执行任何生成内容。
- 工具折叠面板/diff 渲染：仅前端呈现层改动，对输出裁剪（≤32KB→面板≤8 行）只减不增，不扩大日志暴露面。
- **DeVET 后端启动加固（launcher.go）**：此前 `python3` 仅做 LookPath，Windows 上 `WindowsApps\python3.exe` 是 Store 存根（可枚举、不可执行），会以「系统找不到路径」失败；现改为对 `python`/`python3` 候选逐一实测 `--version`，仅执行验证为真的解释器——消除「路径解析与执行不一致」这类 confused-execution 场景的入口。
- 测试加固（sandbox）：Job Object 回收测试的子进程改用唯一命名的 ping 副本，杜绝与其他包并行测试的进程串扰误判。
- 已知边界：`chat --help` 此前在无 TTY 管道下会直接进入 TUI 挂起（可用性缺陷，非越权），已补 help 短路；TUI 交互键位与录屏验收部分未完成，不影响权限语义。

# 2026-08-13 增补：P4-1 DeVET 全链路自动验签（安全属性与边界）

## 新增能力
- task/fleet 子代理完成后自动镜像验签：身份（AID）+ 结果 sha256 承诺 + 工具调用次数 + 写入清单 → 密封 DelegationGrant + CompositeProof → 7 项递归检查（根身份/根链连续/授权哈希/有效期/深度策略/AID 绑定/证明/API 上限）。
- `/chain/tamper`：替换任意委托节点证明后复验，blame_path 精确到 delegation[i]→子代理名；实测 tamper 返回 subagent_proof_invalid + AID 哈希失配详情。
- Web「🛡️ 验证链」面板只读展示（无任何触发验证/篡改的写端点，篡改端点仅在 Bounty 侧测试与演示用，不经 web 暴露）。

## 安全属性
- 验签为纵深防御层：后端宕机/超时（10s）→ 摘要标注「⚠️ 未验证」，**绝不阻断或丢弃子代理结果**，避免可用性被安全组件拖垮；检测到伪造时同样只标注+归因，由父代理（模型）决定处置。
- 承诺绑定：子代理最终结论的 sha256 先于验证计算，验证中 AID 与证明绑定——伪造结果无法通过同一委托节点的证明检查。
- 降级面：镜像请求经本地 127.0.0.1:8765，无新网络暴露面；/chain/mirror 输入长度上限（名称 64/端点 128/承诺 128、agent≤64），防后端内存膨胀。

## 已知边界（诚实声明）
- 证明为「结果承诺级」，非 TLS notary 级网络证明（与 DeVET 演示后端同口径）；防的是子结果伪造/委托替换/策略绕过，不防模型本身被提示词注入欺骗后的"心甘情愿"输出。
- 后端为进程内内存态（重启丢链），多会话并发 mirror 会覆盖上一条链——单进程单会话部署下无影响，多租户部署需按会话分片（未做）。
- 镜像频率=子代理完成频率，无批处理/去重，高并发 fleet（64 子代理）会逐次 POST，吞吐未压测。

# 2026-08-13 增补：P4-2 技能安全审计 + 内置技能

- 新增 `skill/audit.go`：8 条审计规则——recursive-delete / pipe-to-shell / privilege-escalation / history-rewrite / fork-bomb / base64-decode-exec / credential-exfil / network-download-exec。大小写不敏感子串匹配，命中即拒绝加载并回显 40 字上下文片段（可审计、可复核）。
- `Store.Discover` 集成：危险技能文件进入 `Rejected`（名称/路径/Findings），不进入索引、不出现在系统提示；boot 启动时 stderr 报出。
- 顺带接线 `disabled_skills` 配置过滤（此前字段存在但未生效），管理员可显式下线技能。
- 内置 `skills/` 20 个技能全部通过审计（测试锁定：`TestBuiltinSkillsIndexTwenty` 断言索引=20 且 Rejected=0）。
- 已知边界：子串扫描可被拆词/编码/间接引用绕过（非 AST 级语义分析）；技能正文当前不进自动执行链（索引级技能），因此被拒绝技能的最大风险面是「提示词注入式引导」，拒绝加载足以消除该面；若未来支持「触发即注入正文」，需将审计升级为 AST/行为级。

# 2026-08-13 增补：P4-3 后台长命令 + CI（安全属性与边界）

- 后台命令与同步命令**同一条权限预检链路**：PolicyCheck（路径/网络策略）在后台化之前执行；沙箱包装（API Key 环境剥离、Windows Job Object 进程树击杀）同样生效。
- 进程内后台任务上限 16（`maxBackgroundJobs`），超限直接拒绝启动，防 agent 失控 fork；`bash_kill` 走 Job Object 全杀 / taskkill /T /F，可终止任务树。
- `bash_output` 为只读工具（不改变任务状态），输出单次上限 16000 字符、日志尾部读取上限 256KB，防超大日志撑爆上下文。
- 已知边界：后台任务生命周期=进程生命周期（bounty 退出任务即结束，无跨重启持久化/防逃逸后驻留扫描）；后台日志落系统临时目录，含命令输出（可能含敏感内容），单机多用户场景应视为共享敏感面（当前默认单用户）；CI 的 pr-review 示例需要仓库配置 secrets.BOUNTY_API_KEY 才执行模型审查，密钥仅经 GitHub Secrets 注入，不进仓库。

# 2026-08-13 增补：P4-4 多模态输入（安全属性与边界）

- 图片输入白名单：仅 png/jpeg/gif/webp 四种 mime（嗅探文件头 + 扩展名兜底），单图 ≤10MiB，超限/未知类型直接拒绝，防超大 payload 撑爆上下文或内存。
- 图片以 base64 data URL 进入 provider 请求，密钥/图片均不落日志；与文本消息同走既有 SSRF/密钥剥离链路。
- 已知边界：base64 放大 4/3 倍，10MiB 图片约占 13M 字符，超大上下文模型需自行留意 token 消耗（usage 按 API 返回计）；图片消息不写入 SQLite 会话存储（重启后丢失，属隐私友好而非缺陷）；模型对图片内容的理解不受 DeVET 验签约束（验签对象是工具/子代理结果承诺，不是视觉理解正确性）。
