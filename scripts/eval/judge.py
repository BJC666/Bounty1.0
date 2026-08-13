# -*- coding: utf-8 -*-
"""Bounty Eval Judge.

For one completed runner output directory:
  - A tasks: final_text must contain all critical_patterns (and >=1 any_of).
  - B/C tasks: check command must exit 0 in the (agent-modified) src dir;
    forbidden files must be untouched; C tasks additionally obey max_diff_lines.

Usage:
    python judge.py --run work/20260813-000000 [--tasks tasks.json]
"""
import argparse
import difflib
import fnmatch
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent

IGNORE_NAMES = {"bounty.toml", "__pycache__", ".pytest_cache", "node_modules", ".git"}
IGNORE_SUFFIXES = (".pyc", ".pyo")


def load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save_json(path, obj):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(obj, f, ensure_ascii=False, indent=2)


def is_ignored(rel: str) -> bool:
    parts = rel.replace("\\", "/").split("/")
    for p in parts:
        if p in IGNORE_NAMES:
            return True
    return rel.endswith(IGNORE_SUFFIXES)


def hash_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def tree_hashes(root: Path):
    out = {}
    if not root.exists():
        return out
    for p in root.rglob("*"):
        if p.is_file():
            rel = p.relative_to(root).as_posix()
            if not is_ignored(rel):
                out[rel] = hash_file(p)
    return out


def match_forbidden(rel, patterns):
    for pat in patterns:
        norm = pat.replace("\\", "/")
        if fnmatch.fnmatchcase(rel, norm):
            return pat
    return None


def diff_lines_added_removed(a_text, b_text):
    """Count added+removed lines in unified diff of two texts."""
    a_lines = a_text.splitlines(keepends=True)
    b_lines = b_text.splitlines(keepends=True)
    n = 0
    for line in difflib.unified_diff(a_lines, b_lines, lineterm=""):
        if line.startswith(("+", "-")) and not line.startswith(("+++", "---")):
            n += 1
    return n


def compare_dirs(pristine: Path, src: Path):
    """Return (changed_files, forbidden_hits, diff_lines)."""
    before = tree_hashes(pristine)
    after = tree_hashes(src)
    changed = []
    for rel in sorted(set(before) | set(after)):
        if before.get(rel) != after.get(rel):
            changed.append(rel)
    return changed


def run_check(check, src, timeout=300):
    if not check:
        return 0, ""
    env = os.environ.copy()
    env["PYTHONUTF8"] = "1"
    try:
        if os.name == "nt":
            r = subprocess.run(
                subprocess.list2cmdline(check), cwd=str(src), timeout=timeout,
                capture_output=True, env=env, shell=True,
            )
        else:
            r = subprocess.run(
                check, cwd=str(src), timeout=timeout,
                capture_output=True, env=env,
            )
        out = (r.stdout.decode("utf-8", errors="replace")
               + "\n" + r.stderr.decode("utf-8", errors="replace"))
        return r.returncode, out[-6000:]
    except subprocess.TimeoutExpired as e:
        out = ((e.stdout or b"") + b"\n" + (e.stderr or b"")).decode("utf-8", errors="replace")
        return -9, out[-6000:]


def text_file(path):
    try:
        path.read_text(encoding="utf-8")
        return True
    except (UnicodeDecodeError, OSError):
        return False


def judge_a(task, run):
    final = run.get("final_text") or ""
    low = final.lower()
    missing = [p for p in task["critical_patterns"] if p.lower() not in low]
    any_missing = []
    if task.get("any_of"):
        any_missing = [p for p in task["any_of"] if p.lower() not in low]
        any_ok = len(any_missing) < len(task["any_of"])
    else:
        any_ok = True
    if not missing and any_ok:
        return True, "关键点全部命中"
    return False, "未命中关键点: missing=" + str(missing) + " any_of_missing=" + str(any_missing)


def judge_bc(task, run, src, pristine):
    check_rc, check_out = run_check(task["check"], src)
    if check_rc != 0:
        return False, f"测试命令失败 rc={check_rc}", check_rc, check_out, [], [], 0

    changed = compare_dirs(pristine, src)
    forbidden_hits = []
    for rel in changed:
        pat = match_forbidden(rel, task.get("forbidden_files") or [])
        if pat:
            forbidden_hits.append((rel, pat))
    if forbidden_hits:
        return False, "禁改文件被改动: " + str(forbidden_hits), check_rc, check_out, changed, forbidden_hits, None

    diff_lines = 0
    binary_hit = None
    for rel in changed:
        p = pristine / rel
        s = src / rel
        if not p.exists():
            a = ""
        elif not text_file(p):
            binary_hit = rel
            continue
        else:
            a = p.read_text(encoding="utf-8")
        if not s.exists():
            b = ""
        elif not text_file(s):
            binary_hit = rel
            continue
        else:
            b = s.read_text(encoding="utf-8")
        diff_lines += diff_lines_added_removed(a, b)

    if binary_hit:
        return False, f"检测到二进制文件变动: {binary_hit}", check_rc, check_out, changed, forbidden_hits, diff_lines

    budget = task.get("max_diff_lines")
    if budget is not None and diff_lines > budget:
        return False, f"diff 行数 {diff_lines} 超过预算 {budget}", check_rc, check_out, changed, forbidden_hits, diff_lines
    return True, f"测试通过，diff={diff_lines} 行", check_rc, check_out, changed, forbidden_hits, diff_lines


def judge_one(task, task_dir):
    run_json = task_dir / "run.json"
    if not run_json.exists():
        return None
    run = load_json(run_json)
    src = task_dir / "src"
    pristine = task_dir / "pristine"

    if run.get("timeout"):
        verdict, reason = False, f"runner 超时 ({run.get('wall_seconds')}s)"
        return {**base(run), "verdict": verdict, "reason": reason}

    if run.get("exit_code") not in (0, None):
        stderr_tail = ""
        st = task_dir / "stderr.txt"
        if st.exists():
            stderr_tail = st.read_text(encoding="utf-8", errors="replace")[-800:]
        reason = f"bounty 进程异常退出 exit={run['exit_code']}: {stderr_tail.strip()}"
        note = ""
        if task["category"] in ("B", "C") and task.get("check"):
            rc, _ = run_check(task["check"], src)
            note = f"（产物自检：{'测试通过' if rc == 0 else '测试失败 rc=' + str(rc)}）"
        return {**base(run), "verdict": False, "reason": reason + note}

    if task["category"] in ("A", "D"):
        ok, reason = judge_a(task, run)
        return {**base(run), "verdict": ok, "reason": reason,
                "check_rc": None, "check_output_tail": "",
                "changed_files": compare_dirs(pristine, src),
                "forbidden_hits": [], "diff_lines": None}

    ok, reason, rc, out, changed, fh, dl = judge_bc(task, run, src, pristine)
    return {**base(run), "verdict": ok, "reason": reason,
            "check_rc": rc, "check_output_tail": out,
            "changed_files": changed, "forbidden_hits": fh, "diff_lines": dl}


def base(run):
    return {
        "task_id": run.get("task_id"),
        "category": run.get("category"),
        "fixture": run.get("fixture"),
        "title": run.get("title"),
        "model": run.get("model"),
        "steps": run.get("steps"),
        "input_tokens": run.get("input_tokens"),
        "output_tokens": run.get("output_tokens"),
        "tool_calls": run.get("tool_calls"),
        "n_tool_errors": run.get("n_tool_errors"),
        "wall_seconds": run.get("wall_seconds"),
        "timeout": run.get("timeout"),
        "final_text": (run.get("final_text") or "")[:500],
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--run", type=Path, required=True)
    ap.add_argument("--tasks", type=Path, default=EVAL_DIR / "tasks.json")
    args = ap.parse_args()

    tasks = {t["id"]: t for t in load_json(args.tasks)["tasks"]}
    for model_dir in sorted(args.run.iterdir()):
        if not model_dir.is_dir() or model_dir.name.startswith("."):
            continue
        for task_dir in sorted(model_dir.iterdir()):
            if not task_dir.is_dir():
                continue
            task = tasks.get(task_dir.name)
            if not task:
                continue
            result = judge_one(task, task_dir)
            if result is None:
                continue
            save_json(task_dir / "judge.json", result)
            mark = "PASS" if result["verdict"] else "FAIL"
            print(f"[{mark}] {result['model']} {result['task_id']} {result['reason']}")


if __name__ == "__main__":
    main()
