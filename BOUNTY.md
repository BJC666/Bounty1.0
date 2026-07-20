# Bounty Agent

通用 AI 智能体框架。独立设计，整合业界最佳实践，不绑定任何特定 AI 提供商或平台。

## 设计理念

Bounty 是一个独立的智能体框架，你可以选择任何 LLM Provider（DeepSeek、Anthropic、OpenAI、Ollama...）、任何消息通道、任何工具组合。框架本身保持中立，所有具体实现都是可插拔的。

## 架构

- **Phase 1**: 纯 Go 核心（Agent 循环 + 工具 + Provider + 记忆 + Hook + 权限 + 子代理 + CLI）
- **Phase 2**: Go + TypeScript 插件层（goja 内嵌 JS 运行时）
- **Phase 3**: Go + TS + Python AI 微服务（MCP 协议桥接）

## 技术栈

- Go 1.25+ (CGO_ENABLED=0 单二进制)
- SQLite + FTS5 (WAL 模式)
- TOML 配置
- Bubbletea TUI

## 项目结构

```
cmd/bounty/           - 主入口
internal/
├── agent/            - Agent 循环 + 子代理 + 压缩 + 自改进
├── boot/             - 组装 Controller（配置 → 可运行的 Controller）
├── checkpoint/       - git-free 快照 + 回退
├── cli/              - 终端 TUI (Bubbletea)
├── config/           - TOML 配置加载
├── control/          - Controller + Turn 编排（传输无关）
├── environment/      - 环境探针（OS/Shell/工具版本）
├── event/            - 事件类型 + Sink 接口
├── guardian/         - 自动审批守护
├── history/          - 会话 CRUD + FTS5 搜索
├── hook/             - Hook 系统（9 事件）
├── memory/           - 层次化记忆加载 + 自动记忆
├── permission/       - Gate 接口 + 4 种姿态
├── plugin/           - 插件发现 + 加载 + 生命周期
├── provider/         - Provider 接口 + CanonicalizeSchema
│   ├── openai/       - OpenAI 兼容协议（DeepSeek 等）
│   └── anthropic/    - Anthropic Messages API 原生
├── repair/           - 配置修复 + 最后已知良好快照
├── sandbox/          - OS 级沙箱（文件系统 + 网络限制）
├── secrets/          - API Key 保护
├── serve/            - HTTP/SSE 服务 + 嵌入 SPA
├── skill/            - 技能系统（发现 + 索引 + 安全审计 + 生命周期）
├── store/            - SQLite 持久化（WAL + Schema 迁移）
└── tool/             - 工具接口 + Registry
    └── builtin/      - 内置工具（bash/read/write/edit/grep/glob/web/todo）
```

## 参考项目（仅学习参考，非依赖）

源码位于同级目录:
- `../openclaw/` — 多通道 AI 网关（MIT）
- `../hermes-agent/` — 自进化 AI 智能体（MIT）
- `../DeepSeek-Reasonix/` — 缓存优先编程助手（MIT）
- `../claude-code/` — 插件/Hook 架构（专用协议）

## 设计文档

完整设计文档: `docs/specs/2026-07-20-bounty-agent-design.md`
