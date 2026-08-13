# Bounty Eval 基线（阶段 0 交付）

> 生成日期：2026-08-13 | 任务集：`scripts/eval/tasks.json`（49 题：A10/B10/C10/D3/E7/F6/G3）
> 判定规则：A=关键点命中；B=测试命令通过+禁改文件未动；C=测试通过+diff 行数≤预算；全部限 max_steps=50

## 结果

| 模型 | pass@1 | A 仓库理解 | B 多文件改动 | C 修 bug | 平均步数 | 平均输入 tok | 平均输出 tok | 工具失败率 | 超时 |
|------|--------|-----------|-------------|---------|---------|-------------|-------------|-----------|------|
| qwen/qwen3.8-max | **96.7% (29/30)** | 100% (10/10) | 90% (9/10) | 100% (10/10) | 5.6 | 21,031 | 1,303 | 7.3% | 0 |
| deepseek/deepseek-v4-pro | 待测（需要有效 `DEEPSEEK_API_KEY`，当前环境 key 无效） | - | - | - | - | - | - | - | - |

详细逐题数据：`docs/eval/baseline-qwen-20260813.md`；历史曲线：`docs/eval/history.csv`。

## qwen 失败题分析

- **B9（实现 median）**：代码产物正确（判定器产物自检=测试通过），但模型把首个版本写错目录后，
  用 `del`/`rm`/`node unlink` 清理误放文件，三种命令均被权限门 deny（fail-closed），
  连试 3 次触发"3 连卡死护栏"退出（exit=1）。判定按异常退出计失败。
  → 对应路线图 S12/P2-4（Windows 命令体验）与"权限拦截可解释"：deny 应一次性说明
  "此命令被权限门拦截且无法清理，请忽略误放文件继续"，避免模型反复换命令试探。

## 复现

```powershell
python scripts/eval/selfcheck.py                                   # 49/49 离线自检
python scripts/eval/runner.py --models qwen/qwen3.8-max            # 跑全量 49 题
python scripts/eval/judge.py --run scripts/eval/work/<run_id>      # 判定
python scripts/eval/report.py --run scripts/eval/work/<run_id>     # 报告
```

## 对比口径（8 周目标）

- 目标：pass@1 达基线 1.5–2 倍且 ≥50%；当前 qwen 96.7%（受 1 道护栏误伤影响，实际产物通过率 100%）。
- 基线已固定，后续阶段（真摘要压缩 / Repo Map / 锚点补丁等）每轮回归同一任务集。
