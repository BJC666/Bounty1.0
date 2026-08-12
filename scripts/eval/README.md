# Bounty Eval 平台（阶段 0）

30 任务端到端测试集 + 一键跑分脚本 + 基线报告。全部脚本零依赖（Python 3.10+）。

## 目录

```
scripts/eval/
  tasks.json          # 30 题定义（A 仓库理解 10 / B 多文件改动 10 / C 修 bug 10）
  fixtures/           # 三个构造仓库（go-todo / py-stats / ts-util），含注入 bug 与缺实现
  solutions/          # 每题的参考解法（仅用于 selfcheck，跑分时不会注入）
  config/bounty.toml  # Eval 专用 Bounty 配置（workspace_root 占位符由 runner 替换）
  runner.py           # 复制 fixture -> workdir，调 bounty run --json，解析步数/token/工具失败
  judge.py            # 判定器：A=关键点；B=测试绿+禁改文件未动；C=测试绿+diff 行数<=预算
  report.py           # markdown 报告 + docs/eval/history.csv 历史曲线
  selfcheck.py        # 不调模型的离线自检（pristine 必失败 / solution 必通过）
  work/<run_id>/      # 每模型/每任务的隔离 workdir（git 忽略）
  bin/bounty.exe      # runner 自动 go build 的产物（git 忽略）
```

## 用法

```powershell
# 0) 离线自检（不发任何 API 请求，先确认任务集本身正确）
python scripts/eval/selfcheck.py

# 1) 跑分（模型需要 api_key_env 已配置，见 config/bounty.toml）
python scripts/eval/runner.py --models qwen/qwen3.8-max --task-ids A1,B1,C1
python scripts/eval/runner.py --models deepseek/deepseek-v4-pro --run-id baseline-ds

# 2) 判定
python scripts/eval/judge.py --run scripts/eval/work/<run_id>

# 3) 报告（可合并多个 run；CSV 追加到 docs/eval/history.csv）
python scripts/eval/report.py --run scripts/eval/work/<run_id> --label "xxx 基线"
```

## 判定规则

| 类别 | 判定 |
|------|------|
| A 仓库理解 | 最终回答包含全部 critical_patterns，且至少命中一个 any_of |
| B 多文件改动 | 检查命令退出码 0 + 未改动 forbidden_files（测试文件/清单文件） |
| C 修 bug | 检查命令退出码 0 + 未改动 forbidden_files + diff 行数 <= max_diff_lines |
| 全部 | max_steps=50；runner 超时或 bounty 异常退出（含 3 连卡死护栏）= 失败 |

## 新增题目

在 `tasks.json` 的 `tasks` 数组追加一条（B/C 类需在 fixtures 注入 bug/缺实现并在
solutions 放参考解法），然后跑 `python scripts/eval/selfcheck.py` 验证：

- A 类：`critical_patterns`（必须全部命中，大小写不敏感）+ `any_of`（至少命中一个）。
- B/C 类：`check` 是工作目录下可直接执行的命令（Windows 会经 cmd 执行）；
  `forbidden_files` 支持 `**/*_test.go`、`tests/**` 等 fnmatch 模式。
