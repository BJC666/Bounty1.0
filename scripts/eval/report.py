# -*- coding: utf-8 -*-
"""Aggregate judge results into a markdown report + history CSV.

Usage:
    python report.py --run work/20260813-000000
"""
import argparse
import csv
import json
import sys
from collections import defaultdict
from datetime import datetime
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent
REPO_ROOT = EVAL_DIR.parents[1]

CATEGORY_NAMES = {"A": "仓库理解", "B": "多文件改动", "C": "修 bug", "E": "MCP 工具"}


def collect(run_dirs):
    models = defaultdict(lambda: defaultdict(list))
    for run_dir in run_dirs:
        for task_dir in sorted(run_dir.rglob("judge.json")):
            j = json.loads(task_dir.read_text(encoding="utf-8"))
            models[j["model"]][j["category"]].append(j)
    return models


def pct(passed, total):
    if total == 0:
        return "-"
    return f"{100.0 * passed / total:.1f}%"


def stats(rows):
    if not rows:
        return {}
    def avg(key):
        vals = [r.get(key) for r in rows if isinstance(r.get(key), (int, float))]
        return sum(vals) / len(vals) if vals else 0.0
    calls = sum(r.get("tool_calls") or 0 for r in rows)
    errs = sum(r.get("n_tool_errors") or 0 for r in rows)
    return {
        "total": len(rows),
        "passed": sum(1 for r in rows if r.get("verdict")),
        "avg_steps": avg("steps"),
        "avg_in_tok": avg("input_tokens"),
        "avg_out_tok": avg("output_tokens"),
        "avg_wall": avg("wall_seconds"),
        "tool_err_rate": (100.0 * errs / calls) if calls else 0.0,
        "self_heal_rate": self_heal_rate(rows),
        "timeouts": sum(1 for r in rows if r.get("timeout")),
        "crashes": sum(1 for r in rows if not r.get("timeout") and not r.get("verdict") and "异常退出" in (r.get("reason") or "")),
    }


def self_heal_rate(rows):
    """P2-3 指标：出现过工具错误的任务中，最终判定通过的比例（首轮失败后自愈）。"""
    erred = [r for r in rows if (r.get("n_tool_errors") or 0) > 0]
    if not erred:
        return 0.0
    healed = sum(1 for r in erred if r.get("verdict"))
    return 100.0 * healed / len(erred)


def build_report(run_id, models):
    lines = []
    add = lines.append
    add(f"# Bounty Eval 基线报告（run {run_id}）")
    add("")
    add(f"- 生成时间：{datetime.now().strftime('%Y-%m-%d %H:%M')}")
    add(f"- 任务集：`scripts/eval/tasks.json`（A 仓库理解 10 + B 多文件改动 10 + C 修 bug 10）")
    add("- 判定规则：A=关键点命中；B=测试命令通过+禁改文件未动；C=测试通过+diff 行数≤预算；全部限 max_steps=50")
    add("")

    add("## 总览")
    add("")
    add("| 模型 | pass@1 | A | B | C | 平均步数 | 平均输入 tok | 平均输出 tok | 工具失败率 | 自愈率 | 超时数 |")
    add("|---|---|---|---|---|---|---|---|---|---|---|")
    for model in sorted(models):
        cats = models[model]
        all_rows = [r for v in cats.values() for r in v]
        s = stats(all_rows)
        a = stats(cats.get("A", [])); b = stats(cats.get("B", [])); c = stats(cats.get("C", []))
        add(f"| {model} | {pct(s['passed'], s['total'])} "
            f"| {pct(a['passed'], a['total'])} | {pct(b['passed'], b['total'])} | {pct(c['passed'], c['total'])} "
            f"| {s['avg_steps']:.1f} | {s['avg_in_tok']:.0f} | {s['avg_out_tok']:.0f} "
            f"| {s['tool_err_rate']:.1f}% | {s['self_heal_rate']:.1f}% | {s['timeouts']} |")
    add("")

    for model in sorted(models):
        cats = models[model]
        add(f"## 模型 {model}")
        add("")
        add("| 任务 | 类别 | 判定 | 步数 | 输入 tok | 输出 tok | 工具失败 | 用时(s) | 原因/备注 |")
        add("|---|---|---|---|---|---|---|---|---|")
        for cat in ("A", "B", "C", "E"):
            for r in sorted(cats.get(cat, []), key=lambda x: x["task_id"]):
                mark = "通过" if r["verdict"] else "失败"
                reason = (r.get("reason") or "")[:80].replace("|", "/")
                add(f"| {r['task_id']} {r['title']} | {CATEGORY_NAMES[cat]} | {mark} "
                    f"| {r.get('steps')} | {r.get('input_tokens')} | {r.get('output_tokens')} "
                    f"| {r.get('n_tool_errors')} | {r.get('wall_seconds')} | {reason} |")
        add("")

    add("## 失败明细")
    add("")
    found = False
    for model in sorted(models):
        for cat in ("A", "B", "C", "E"):
            for r in sorted(models[model].get(cat, []), key=lambda x: x["task_id"]):
                if r.get("verdict"):
                    continue
                found = True
                add(f"### {model} / {r['task_id']} {r['title']}")
                add("")
                add(f"- 原因：{r.get('reason')}")
                if r.get("check_output_tail"):
                    add(f"- 测试输出（尾部）：\n```\n{r['check_output_tail'][-2000:]}\n```")
                if r.get("changed_files"):
                    add(f"- 改动文件：{r['changed_files']}")
                add("")
    if not found:
        add("（无）")
    return "\n".join(lines)


def append_history(run_id, models, csv_path):
    csv_path.parent.mkdir(parents=True, exist_ok=True)
    new_file = not csv_path.exists()
    with open(csv_path, "a", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        if new_file:
            w.writerow(["date", "run_id", "model", "category", "total", "passed", "pass_rate",
                        "avg_steps", "avg_in_tok", "avg_out_tok", "tool_err_rate", "avg_wall_s", "timeouts"])
        today = datetime.now().strftime("%Y-%m-%d")
        for model in sorted(models):
            cats = models[model]
            for cat in ("A", "B", "C", "ALL"):
                rows = ([r for v in cats.values() for r in v] if cat == "ALL" else cats.get(cat, []))
                s = stats(rows)
                w.writerow([today, run_id, model, cat, s["total"], s["passed"],
                            round(100.0 * s["passed"] / s["total"], 1) if s["total"] else "",
                            round(s["avg_steps"], 1), round(s["avg_in_tok"]), round(s["avg_out_tok"]),
                            round(s["tool_err_rate"], 1), round(s["avg_wall"], 1), s["timeouts"]])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--run", type=Path, nargs="+", required=True)
    ap.add_argument("--out", type=Path, default=None)
    ap.add_argument("--label", default=None, help="run label used in the report title")
    ap.add_argument("--csv", type=Path, default=REPO_ROOT / "docs" / "eval" / "history.csv")
    args = ap.parse_args()

    models = collect(args.run)
    if not models:
        sys.exit("no judge.json found; run judge.py first")
    run_id = args.label or "+".join(r.name for r in args.run)
    md = build_report(run_id, models)
    out = args.out or (REPO_ROOT / "docs" / "eval" / f"{run_id}-report.md")
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(md, encoding="utf-8")
    append_history(run_id, models, args.csv)
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass
    print(md)
    print(f"[saved] {out}")
    print(f"[csv] {args.csv}")


if __name__ == "__main__":
    main()
