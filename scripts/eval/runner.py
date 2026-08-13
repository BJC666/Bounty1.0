# -*- coding: utf-8 -*-
"""Bounty Eval Runner.

Copies a fixture into an isolated workdir, runs `bounty run --json` against it,
and parses the JSONL transcript into per-task metrics (steps / tokens / tool
errors / final text).

Usage:
    python runner.py --models deepseek/deepseek-v4-pro,qwen/qwen3.8-max
    python runner.py --task-ids A1,B1,C1 --models deepseek/deepseek-v4-flash
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent
REPO_ROOT = EVAL_DIR.parents[1]

COPY_IGNORE_DIRS = {"__pycache__", ".pytest_cache", "node_modules", ".git"}
COPY_IGNORE_SUFFIXES = (".pyc", ".pyo")


def copy_ignore(dirpath, names):
    out = []
    for n in names:
        if n in COPY_IGNORE_DIRS or n.endswith(COPY_IGNORE_SUFFIXES):
            out.append(n)
    return out


def load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save_json(path, obj):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(obj, f, ensure_ascii=False, indent=2)


def model_slug(model):
    return model.replace("/", "__")


def ensure_binary(repo_root, force=False):
    bin_dir = EVAL_DIR / "bin"
    bin_dir.mkdir(exist_ok=True)
    exe = bin_dir / ("bounty.exe" if os.name == "nt" else "bounty")
    if force or not exe.exists():
        print(f"[build] go build -o {exe} ./cmd/bounty")
        r = subprocess.run(
            ["go", "build", "-o", str(exe), "./cmd/bounty"],
            cwd=str(repo_root), capture_output=True, text=True, encoding="utf-8", errors="replace",
        )
        if r.returncode != 0:
            sys.exit(f"go build failed:\n{r.stdout}\n{r.stderr}")
    return exe


def parse_transcript(text):
    """Parse bounty run --json output into metrics."""
    events = []
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            events.append(None)

    steps = 0
    text_parts = []
    in_tok = out_tok = 0
    cache_hits = 0
    tool_calls = 0
    tool_errors = []
    tools_used = []
    turns_complete = 0
    final_err = None
    first_error_step = None
    for ev in events:
        if not isinstance(ev, dict):
            continue
        t = ev.get("type")
        if t == "step":
            steps += 1
        elif t == "text":
            text_parts.append(ev.get("text_delta") or "")
        elif t == "usage":
            in_tok += ev.get("input_tokens") or 0
            out_tok += ev.get("output_tokens") or 0
            if ev.get("cache_hit"):
                cache_hits += 1
        elif t == "tool_call":
            tool_calls += 1
            if ev.get("tool_name"):
                tools_used.append(ev.get("tool_name"))
        elif t == "tool_result":
            if ev.get("tool_err"):
                tool_errors.append({
                    "tool": ev.get("tool_name"),
                    "err": (ev.get("tool_err") or "")[:300],
                })
                if first_error_step is None:
                    first_error_step = steps
        elif t == "turn_complete":
            turns_complete += 1
            if ev.get("turn_err"):
                final_err = ev.get("turn_err")

    return {
        "events": len(events),
        "steps": steps,
        "final_text": "".join(text_parts),
        "input_tokens": in_tok,
        "output_tokens": out_tok,
        "cache_hit_events": cache_hits,
        "tool_calls": tool_calls,
        "tool_errors": tool_errors,
        "tools_used": tools_used,
        "n_tool_errors": len(tool_errors),
        "first_error_step": first_error_step,
        "turns_complete": turns_complete,
        "final_err": final_err,
    }


def run_one(args, task, model, run_id, bounty_bin):
    slug = model_slug(model)
    task_dir = args.work / run_id / slug / task["id"]
    src = task_dir / "src"
    pristine = task_dir / "pristine"
    run_json = task_dir / "run.json"
    if run_json.exists() and not args.redo:
        print(f"[skip] {model} {task['id']} (already ran)")
        return task["id"], model, None

    if task_dir.exists():
        shutil.rmtree(task_dir)
    task_dir.mkdir(parents=True)
    fixture = EVAL_DIR / "fixtures" / task["fixture"]
    shutil.copytree(fixture, src, ignore=copy_ignore)
    shutil.copytree(src, pristine, ignore=copy_ignore)
    cfg_text = args.config.read_text(encoding="utf-8")
    cfg_text = cfg_text.replace("__WORKSPACE_ROOT__", src.resolve().as_posix())
    (src / "bounty.toml").write_text(cfg_text, encoding="utf-8")

    cmd = [
        str(bounty_bin), "run", task["prompt"],
        "--json", "--model=" + model, "--max-steps=" + str(args.max_steps),
    ]
    started = time.time()
    timeout = False
    exit_code = None
    stdout = ""
    stderr = ""
    try:
        r = subprocess.run(
            cmd, cwd=str(src), timeout=args.timeout,
            capture_output=True, env=os.environ.copy(),
        )
        exit_code = r.returncode
        stdout = r.stdout.decode("utf-8", errors="replace")
        stderr = r.stderr.decode("utf-8", errors="replace")
    except subprocess.TimeoutExpired as e:
        timeout = True
        stdout = (e.stdout or b"").decode("utf-8", errors="replace")
        stderr = (e.stderr or b"").decode("utf-8", errors="replace")

    wall = round(time.time() - started, 1)
    metrics = parse_transcript(stdout)
    result = {
        "task_id": task["id"],
        "category": task["category"],
        "fixture": task["fixture"],
        "title": task["title"],
        "model": model,
        "prompt": task["prompt"],
        "run_id": run_id,
        "exit_code": exit_code,
        "timeout": timeout,
        "wall_seconds": wall,
        "max_steps": args.max_steps,
        "started_at": datetime.now().isoformat(timespec="seconds"),
        **metrics,
    }
    save_json(run_json, result)
    (task_dir / "transcript.jsonl").write_text(stdout, encoding="utf-8")
    (task_dir / "stderr.txt").write_text(stderr, encoding="utf-8")
    status = "TIMEOUT" if timeout else f"exit={exit_code}"
    print(f"[done] {model} {task['id']} steps={metrics['steps']} "
          f"tok={metrics['input_tokens'] + metrics['output_tokens']} "
          f"toolerr={metrics['n_tool_errors']} wall={wall}s {status}")
    return task["id"], model, result


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tasks", type=Path, default=EVAL_DIR / "tasks.json")
    ap.add_argument("--config", type=Path, default=EVAL_DIR / "config" / "bounty.toml")
    ap.add_argument("--models", default="deepseek/deepseek-v4-pro,qwen/qwen3.8-max")
    ap.add_argument("--task-ids", default="")
    ap.add_argument("--parallel", type=int, default=2)
    ap.add_argument("--timeout", type=int, default=1500, help="per-task seconds")
    ap.add_argument("--max-steps", type=int, default=50)
    ap.add_argument("--work", type=Path, default=EVAL_DIR / "work")
    ap.add_argument("--run-id", default=datetime.now().strftime("%Y%m%d-%H%M%S"))
    ap.add_argument("--rebuild", action="store_true")
    ap.add_argument("--redo", action="store_true", help="re-run tasks that already have results")
    args = ap.parse_args()

    data = load_json(args.tasks)
    tasks = data["tasks"]
    models = [m.strip() for m in args.models.split(",") if m.strip()]
    ids = [i.strip() for i in args.task_ids.split(",") if i.strip()]
    if ids:
        tasks = [t for t in tasks if t["id"] in ids]
    if not tasks:
        sys.exit("no tasks selected")

    bounty_bin = ensure_binary(REPO_ROOT, force=args.rebuild)
    args.work.mkdir(parents=True, exist_ok=True)
    (args.work / args.run_id).mkdir(parents=True, exist_ok=True)
    meta = {
        "run_id": args.run_id,
        "models": models,
        "task_ids": [t["id"] for t in tasks],
        "max_steps": args.max_steps,
        "timeout": args.timeout,
        "started_at": datetime.now().isoformat(timespec="seconds"),
    }
    save_json(args.work / args.run_id / "meta.json", meta)

    print(f"[run] {args.run_id} models={models} tasks={len(tasks)} parallel={args.parallel}")
    with ThreadPoolExecutor(max_workers=args.parallel) as pool:
        futures = [
            pool.submit(run_one, args, task, model, args.run_id, bounty_bin)
            for model in models for task in tasks
        ]
        for fut in as_completed(futures):
            try:
                fut.result()
            except Exception as e:  # keep other tasks alive
                print(f"[error] {e}")

    print(f"[run done] {args.run_id}")


if __name__ == "__main__":
    main()
