# -*- coding: utf-8 -*-
"""P8-6 Eval 周报：全量跑分 → history.csv 追加 → 周报 md（环比 + 倒退标红）。

用法：
    python scripts/eval/weekly.py                 # 默认串行全量跑 qwen 并出周报
    python scripts/eval/weekly.py --run-dir C:\\bounty-eval\\work\\20260813-000000   # 复用已有 run
    python scripts/eval/weekly.py --model qwen/qwen3.8-max --parallel 1

周报输出：docs/eval/weekly-<run_id>.md。环比对象 = history.csv 中同模型上一次
全量（ALL）记录；pass@1/步数/token/失败率任一倒退即标红并给出红线结论。
"""

import argparse
import csv
import json
import subprocess
import sys
from collections import defaultdict
from datetime import datetime
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent
REPO_ROOT = EVAL_DIR.parents[1]
DEFAULT_WORK = Path(r"C:\bounty-eval\work")
HISTORY_CSV = REPO_ROOT / "docs" / "eval" / "history.csv"
REPORT_DIR = REPO_ROOT / "docs" / "eval"

sys.path.insert(0, str(EVAL_DIR))
from report import collect, stats, pct  # noqa: E402  (import after path setup)


def run_full_eval(model, parallel, work, run_id):
    cmd = [
        sys.executable, str(EVAL_DIR / "runner.py"),
        "--models", model,
        "--work", str(work),
        "--run-id", run_id,
        "--parallel", str(parallel),
    ]
    print(f"[weekly] running: {' '.join(cmd)}")
    r = subprocess.run(cmd, cwd=str(EVAL_DIR))
    if r.returncode != 0:
        sys.exit(f"[weekly] runner failed (exit {r.returncode})")
    run_dir = work / run_id
    judge_cmd = [
        sys.executable, str(EVAL_DIR / "judge.py"),
        "--run", str(run_dir), "--models", model,
    ]
    print(f"[weekly] judging: {' '.join(judge_cmd)}")
    r = subprocess.run(judge_cmd, cwd=str(EVAL_DIR))
    if r.returncode != 0:
        sys.exit(f"[weekly] judge failed (exit {r.returncode})")
    return run_dir


def load_history_rows(csv_path):
    rows = []
    if not csv_path.exists():
        return rows
    with open(csv_path, encoding="utf-8", newline="") as f:
        for r in csv.DictReader(f):
            rows.append(r)
    return rows


def last_full_by_model(rows, model, exclude_run_id=None):
    """history.csv 中该模型最近一次 ALL 类记录（环比基准，排除当前 run 自身）。"""
    alls = [r for r in rows
            if r["model"] == model and r["category"] == "ALL"
            and r["run_id"].lower() != (exclude_run_id or "").lower()]
    return alls[-1] if alls else None


def fmt(v, digits=1):
    try:
        return f"{float(v):.{digits}f}"
    except (TypeError, ValueError):
        return "-"


def build_weekly(run_id, model, run_dir, prev):
    models = collect([run_dir])
    if model not in models:
        sys.exit(f"[weekly] no judge results for {model}")
    all_rows = [r for v in models[model].values() for r in v]
    s = stats(all_rows)
    a = stats(models[model].get("A", []))
    b = stats(models[model].get("B", []))
    c = stats(models[model].get("C", []))

    lines = []
    add = lines.append
    add(f"# Bounty Eval 周报（{datetime.now().strftime('%Y-%m-%d')}）")
    add("")
    add(f"- run_id：`{run_id}`（模型 {model}，50 题全量）")
    add(f"- 生成时间：{datetime.now().strftime('%Y-%m-%d %H:%M')}")
    add("")

    def delta(cur, base, lower_better):
        """环比差值；倒退（对 lower_better 指标上升、对 pass@1 下降）标红。"""
        if base is None:
            return "-", False
        d = round(float(cur), 1) - round(float(base), 1)
        bad = (d > 0) if lower_better else (d < 0)
        sign = "+" if d > 0 else ""
        txt = f"{sign}{d:.1f}"
        return (f"<span style='color:red'>**{txt}**</span>" if bad else txt), bad

    add("## 总览（vs 上一轮全量）")
    add("")
    add("| 指标 | 本轮 | 上轮 | 环比 |")
    add("|---|---|---|---|")
    d_pass, bad_pass = delta(s["pass_rate"] if "pass_rate" in s else 100.0 * s["passed"] / s["total"],
                              prev["pass_rate"] if prev else None, lower_better=False)
    d_steps, bad_steps = delta(s["avg_steps"], prev["avg_steps"] if prev else None, lower_better=True)
    d_in, bad_in = delta(s["avg_in_tok"], prev["avg_in_tok"] if prev else None, lower_better=True)
    d_err, bad_err = delta(s["tool_err_rate"], prev["tool_err_rate"] if prev else None, lower_better=True)
    add(f"| pass@1 | {pct(s['passed'], s['total'])} | {prev['pass_rate'] if prev else '-'} | {d_pass} |")
    add(f"| 平均步数 | {s['avg_steps']:.1f} | {fmt(prev['avg_steps']) if prev else '-'} | {d_steps} |")
    add(f"| 平均输入 tok | {s['avg_in_tok']:.0f} | {fmt(prev['avg_in_tok'], 0) if prev else '-'} | {d_in} |")
    add(f"| 工具调用失败率 | {s['tool_err_rate']:.1f}% | {fmt(prev['tool_err_rate']) if prev else '-'}% | {d_err} |")
    add(f"| 平均输出 tok | {s['avg_out_tok']:.0f} | {fmt(prev['avg_out_tok'], 0) if prev else '-'} | - |")
    add("")

    add("## 分赛道")
    add("")
    add("| 赛道 | pass@1 | 平均步数 | 平均输入 tok |")
    add("|---|---|---|---|")
    for name, cat in (("A 仓库理解", a), ("B 多文件改动", b), ("C 修 bug", c)):
        add(f"| {name} | {pct(cat['passed'], cat['total'])} | {cat['avg_steps']:.1f} | {cat['avg_in_tok']:.0f} |")
    add("")

    regressions = [x for x in (bad_pass, bad_steps, bad_in, bad_err) if x]
    if regressions:
        add("## ⛔ 红线判断：**存在倒退**")
        add("")
        add("环比出现倒退（pass@1 下降或步数/token/失败率上升），**禁止发布**。"
            "需定位回归源（对比失败明细/工具错误分类）后重跑验证。")
        add("")
    else:
        add("## ✅ 红线判断：无倒退")
        add("")
        add("所有指标环比持平或改善，可继续发布流程。")
        add("")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="qwen/qwen3.8-max")
    ap.add_argument("--work", type=Path, default=DEFAULT_WORK)
    default_run_id = datetime.now().strftime("weekly-%Y%m%d-%H%M%S")
    ap.add_argument("--run-id", default=default_run_id)
    ap.add_argument("--parallel", type=int, default=1, help="默认串行（规避并发系统提示串台）")
    ap.add_argument("--run-dir", type=Path, default=None, help="复用已有 run（跳过跑分与判定）")
    ap.add_argument("--prev-run-id", default=None,
                    help="显式指定环比基准 run_id（history.csv 中）；默认取最近一条 ALL")
    args = ap.parse_args()

    if args.run_dir is not None:
        run_dir = args.run_dir
        if args.run_id == default_run_id:
            args.run_id = run_dir.name  # 复用已有 run 时以目录名作为 run_id
    else:
        run_dir = run_full_eval(args.model, args.parallel, args.work, args.run_id)

    rows = load_history_rows(HISTORY_CSV)
    if args.prev_run_id:
        prev = next((r for r in rows
                     if r["run_id"].lower() == args.prev_run_id.lower()
                     and r["category"] == "ALL"), None)
    else:
        prev = last_full_by_model(rows, args.model, exclude_run_id=args.run_id)
    if prev:
        print(f"[weekly] 环比基准：run {prev['run_id']} pass@1={prev['pass_rate']} "
              f"steps={prev['avg_steps']} in_tok={prev['avg_in_tok']} tool_err={prev['tool_err_rate']}%")
    md = build_weekly(args.run_id, args.model, run_dir, prev)
    out = REPORT_DIR / f"weekly-{args.run_id}.md"
    out.write_text(md, encoding="utf-8")
    print(f"[weekly] report written: {out}")
    print("[weekly] 提示：如需入库 history.csv，先执行 report.py --run <run_dir>")


if __name__ == "__main__":
    main()
