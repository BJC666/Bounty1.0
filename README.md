# Bounty 1.0 — AI Agent Framework × DeVET

通用 AI 智能体框架，集成 DeVET 多智能体委托验证系统。

## 快速开始

### 1. 设置 API Key

```powershell
setx DEEPSEEK_API_KEY "sk-your-key"
```

### 2. 一键启动

```powershell
.\start.bat
```

或手动启动：

```powershell
# 终端 1: 启动 DeVET 后端
cd DeVET\backend
python server.py

# 终端 2: 启动 Bounty
bounty.exe chat
```

## Bounty 命令

| 命令 | 用途 |
|------|------|
| `bounty chat` | 交互式 AI 对话 |
| `bounty run "..."` | 单次执行 |
| `bounty doctor` | 配置诊断 |
| `bounty serve` | HTTP Gateway |
| `bounty dashboard` | Web 仪表盘 |

## DeVET 集成工具

Bounty 通过 REST API 调用 DeVET 的验证端点，提供 5 个工具：

| 工具 | 用途 |
|------|------|
| `devet_health` | 检查 DeVET 后端状态 |
| `devet_build_scenario` | 构建 3 智能体 Trading DAO 委托链 |
| `devet_verify_chain` | 验证委托链的完整性和安全性 |
| `devet_list_attacks` | 列出 8 种攻击类型 |
| `devet_simulate_attack` | 模拟攻击并显示检测结果 |

## 对话示例

```
> 帮我验证一下当前的委托链是否安全

Bounty 调用 devet_health → devet_build_scenario → devet_verify_chain
返回：✅ 验证通过，3 个智能体全部可信，无攻击检测

> 模拟一个委托替换攻击

Bounty 调用 devet_simulate_attack(attack_type="A1_delegation_replacement")
返回：⚠️ 攻击已检测！故障类型：grant_tampered，归因：ExecutionAgentETH
```

## 技术栈

- **Bounty**: Go 1.25+, SQLite+FTS5, Bubbletea TUI, 19 工具, 4 Provider
- **DeVET**: Python 3.10+, FastAPI, 30 pytest (全部通过), 8 攻击 100% 检测率

## 项目结构

```
Bounty1.0/
├── bounty.exe          ← Go 单二进制
├── bounty.toml         ← 配置
├── start.bat           ← 一键启动
├── DeVET/              ← DeVET 验证系统（独立运行）
│   ├── backend/        ← FastAPI (:8765)
│   ├── frontend/       ← Web UI (:8766)
│   └── vet-repro/      ← Python 核心库 + 30 tests
└── internal/           ← Bounty Go 源码 (28 packages)
```
