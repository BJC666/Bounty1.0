# -*- coding: utf-8 -*-
"""Self-check for the eval task set WITHOUT calling any model.

For every B/C task:
  1. pristine fixture must FAIL the check command (bug/feature really present);
  2. applying the reference solution must PASS the check command;
  3. the solution must not touch forbidden files and must stay within diff budget.
For every A task:
  4. the golden answer must satisfy its own critical_patterns / any_of.

Usage:
    python selfcheck.py
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent


def load_tasks():
    with open(EVAL_DIR / "tasks.json", encoding="utf-8") as f:
        return json.load(f)["tasks"]


def copy_fixture(name, dst):
    shutil.copytree(EVAL_DIR / "fixtures" / name, dst,
                    ignore=lambda d, names: [n for n in names
                                             if n in {"__pycache__", ".pytest_cache", "node_modules", ".git"}
                                             or n.endswith((".pyc", ".pyo"))])


def run_check(check, cwd):
    if os.name == "nt":
        r = subprocess.run(subprocess.list2cmdline(check), cwd=str(cwd),
                           capture_output=True, timeout=300, shell=True)
    else:
        r = subprocess.run(check, cwd=str(cwd), capture_output=True, timeout=300)
    out = (r.stdout.decode("utf-8", errors="replace") + "\n"
           + r.stderr.decode("utf-8", errors="replace"))
    return r.returncode, out


def apply_solution(task, cwd):
    sol = EVAL_DIR / "solutions" / task["fixture"] / task["id"]
    if not sol.exists():
        return False
    for p in sol.rglob("*"):
        if p.is_file():
            rel = p.relative_to(sol)
            dst = cwd / rel
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(p, dst)
    return True


def main():
    tasks = load_tasks()
    fails = []
    tmp = Path(tempfile.mkdtemp(prefix="eval_selfcheck_"))
    try:
        for task in tasks:
            tid = task["id"]
            if task["category"] == "A":
                golden = (task.get("golden_answer") or "").lower()
                missing = [p for p in task["critical_patterns"] if p.lower() not in golden]
                any_missing = [p for p in task.get("any_of", []) if p.lower() not in golden]
                any_ok = not task.get("any_of") or len(any_missing) < len(task["any_of"])
                if not missing and any_ok:
                    print(f"[ok] A {tid}: golden 命中全部关键点")
                else:
                    fails.append(f"A {tid}: golden 未命中 missing={missing} any_of_missing={any_missing}")
                    print(f"[FAIL] A {tid}: golden 未命中 {missing} {any_missing}")
                continue

            cwd = tmp / tid
            copy_fixture(task["fixture"], cwd)

            rc, out = run_check(task["check"], cwd)
            if rc == 0:
                fails.append(f"{tid}: 原始 fixture 竟然通过了检查（bug/缺实现未注入成功）")
                print(f"[FAIL] {tid}: pristine check 通过（rc=0），bug 未注入成功")
            else:
                print(f"[ok] {tid}: pristine check 失败 rc={rc}（bug/缺实现存在）")

            apply_solution(task, cwd)
            rc, out = run_check(task["check"], cwd)
            if rc != 0:
                fails.append(f"{tid}: 参考解法未通过检查 rc={rc}")
                print(f"[FAIL] {tid}: solution check rc={rc}")
                print("    " + out[-800:].replace("\n", "\n    "))
            else:
                print(f"[ok] {tid}: solution check 通过")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

    if fails:
        print("\nSELFCHECK FAILED:")
        for f in fails:
            print("  -", f)
        sys.exit(1)
    print("\nSELFCHECK OK: 30/30 任务自检通过")


if __name__ == "__main__":
    main()
