#!/usr/bin/env python3
"""P8-5 答辩证据链①：Eval 曲线图（P0 基线 → P8-r7）。

数据源：docs/eval/history.csv（qwen 全量 ALL 记录）。
输出：docs/eval/defense-eval-curve.png（pass@1/步数 + 输入 tok/失败率 双图）。
复现：python scripts/eval/defense_curve.py
"""
import csv
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__))) + "/.."
ROOT = os.path.normpath(REPO)
HISTORY = os.path.join(ROOT, "docs", "eval", "history.csv")
OUT = os.path.join(ROOT, "docs", "eval", "defense-eval-curve.png")

plt.rcParams["font.sans-serif"] = ["Microsoft YaHei", "SimHei", "DejaVu Sans"]
plt.rcParams["axes.unicode_minus"] = False

# 展示顺序（含 P0 基线 + P6/P7 收尾 + P8 攻坚轮）
ORDER = [
    ("P0 基线", "qwen3.8-max 基线 2026-08-13"),
    ("P6", "20260813-165619"),
    ("P6 二轮", "20260813-172439"),
    ("P7-1", "20260813-211729"),
    ("P7-2", "20260813-215848"),
    ("P7-3", "20260813-221656"),
    ("P7 收尾", "20260813-222946"),
    ("P8-r1", "P8-r1"),
    ("P8-r2", "P8-r2"),
    ("P8-r3", "P8-r3"),
    ("P8-r4", "P8-r4"),
    ("P8-r5", "P8-r5"),
    ("P8-r6", "P8-r6"),
    ("P8-r7", "P8-r7"),
]

rows = {}
with open(HISTORY, encoding="utf-8") as f:
    for r in csv.DictReader(f):
        if r["model"] != "qwen/qwen3.8-max" or r["category"] != "ALL":
            continue
        rows[r["run_id"]] = r

labels, pass_rate, steps, in_tok, err = [], [], [], [], []
for label, rid in ORDER:
    r = rows.get(rid)
    if r is None:
        continue
    labels.append(label)
    pass_rate.append(float(r["pass_rate"]))
    steps.append(float(r["avg_steps"]))
    in_tok.append(float(r["avg_in_tok"]))
    err.append(float(r["tool_err_rate"]))

fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(11, 7.5), dpi=150)
x = list(range(len(labels)))

# 上图：pass@1 + 步数
ax1.plot(x, pass_rate, "o-", color="#0a7d33", lw=2, label="pass@1 (%)")
ax1.set_ylabel("pass@1 (%)", color="#0a7d33")
ax1.set_ylim(90, 102)
ax1.axhline(100, color="#0a7d33", ls=":", lw=1, alpha=0.5)
ax1a = ax1.twinx()
ax1a.plot(x, steps, "s--", color="#c0392b", lw=2, label="平均步数")
ax1a.set_ylabel("平均步数", color="#c0392b")
ax1a.set_ylim(2, 6)
ax1.set_title("Bounty Eval 演进：pass@1 与平均步数（P0 基线 → P8-r7）", fontsize=13)
ax1.grid(alpha=0.3)

# 下图：输入 tok + 失败率
ax2.plot(x, in_tok, "o-", color="#1f4e9c", lw=2, label="平均输入 tok")
ax2.set_ylabel("平均输入 tok", color="#1f4e9c")
ax2.axhline(14300, color="#1f4e9c", ls="--", lw=1, alpha=0.6)
ax2.text(0.2, 14500, "P8-3 目标线 14,300", fontsize=9, color="#1f4e9c")
ax2a = ax2.twinx()
ax2a.plot(x, err, "^-", color="#8e44ad", lw=1.8, label="工具失败率 (%)")
ax2a.set_ylabel("工具失败率 (%)", color="#8e44ad")
ax2a.set_ylim(0, 10)
ax2.set_title("输入 token 与工具失败率（目标线 14,300 tok / <5%）", fontsize=13)
ax2.grid(alpha=0.3)

for ax in (ax1, ax2):
    ax.set_xticks(x)
    ax.set_xticklabels(labels, rotation=40, ha="right", fontsize=8)

lines1, labs1 = ax1.get_legend_handles_labels()
lines1a, labs1a = ax1a.get_legend_handles_labels()
ax1.legend(lines1 + lines1a, labs1 + labs1a, loc="lower right", fontsize=9)
lines2, labs2 = ax2.get_legend_handles_labels()
lines2a, labs2a = ax2a.get_legend_handles_labels()
ax2.legend(lines2 + lines2a, labs2 + labs2a, loc="upper right", fontsize=9)

fig.tight_layout()
fig.savefig(OUT)
print(f"[saved] {OUT}")
print(f"points: {len(labels)} (pass@1 {pass_rate[0]}% -> {pass_rate[-1]}%; "
      f"steps {steps[0]:.1f} -> {steps[-1]:.1f}; tok {in_tok[0]:.0f} -> {in_tok[-1]:.0f})")
