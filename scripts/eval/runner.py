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
import urllib.error
import urllib.request
from pathlib import Path
try:
    import tomllib
except ImportError:  # pragma: no cover - Python < 3.11
    tomllib = None
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


# --- P7-1 健壮性：网络失败判定 / 错误口径分列 / API 健康预检 ---

NETWORK_ERROR_MARKERS = (
    "network error", "no such host", "dial tcp", "connection refused",
    "connection reset", "lookup ", "temporary failure in name resolution",
    "timed out", "request timeout", "http 429", "too many requests",
    "http 500", "http 502", "http 503", "bad gateway", "service unavailable",
)


def is_network_failure(stderr, stdout=""):
    """True 表示本次任务失败属于网络/DNS/服务端瞬断，可安全重试。"""
    text = ((stderr or "") + " " + (stdout or "")).lower()
    return any(m in text for m in NETWORK_ERROR_MARKERS)


_TOOL_FAIL_MARKERS = (
    "不是内部或外部命令", "系统找不到", "找不到文件", "no such file",
    "拒绝访问", "access is denied", "permission denied",
    "文件名、目录名或卷标语法不正确", "无法打开", "is not recognized",
)


def classify_tool_error(err):
    """区分「工具调用失败」与「验证失败（bash 执行了测试/命令但结果非零）」。

    - 含 --- FAIL / exit status 且无工具性特征 → verify（测试没通过，bash 本身正常）
    - 其余（文件不存在/编码/权限/未知命令等）→ tool（工具调用本身失败）
    """
    e = (err or "").lower()
    if "--- fail" in e:
        return "verify"
    if "exit status" in e and not any(m in e for m in _TOOL_FAIL_MARKERS):
        return "verify"
    return "tool"


def parse_providers(config_path):
    """从 eval bounty.toml 解析 {model: base_url}，用于健康预检。"""
    if tomllib is None:
        return {}
    with open(config_path, "rb") as f:
        data = tomllib.load(f)
    out = {}
    for prov in data.get("providers", []):
        base = prov.get("base_url", "")
        for m in prov.get("models", []):
            out[f"{prov.get('name')}/{m}"] = base
    return out


def preflight_models(config_path, models, timeout=10):
    """对每个模型 base_url 发轻量 GET /models：401/403/404=端点可达，网络异常=失败。

    返回 True=全部可达（或无法解析的模型被跳过）；False=存在网络级失败。
    """
    urls = parse_providers(config_path)
    unknown = [m for m in models if not urls.get(m)]
    if unknown:
        print(f"[preflight] WARN 未在 config 中找到 base_url，跳过探测：{unknown}")
    ok = True
    for m in models:
        base = urls.get(m)
        if not base:
            continue
        url = base.rstrip("/") + "/models"
        try:
            req = urllib.request.Request(
                url, headers={"Authorization": "Bearer sk-preflight-invalid"})
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                print(f"[preflight] OK {m} ({base}) HTTP {resp.status}（端点可达）")
        except urllib.error.HTTPError as e:
            # 401/403/404 均说明端点可达（key 无效或被拒），预检通过
            print(f"[preflight] OK {m} ({base}) HTTP {e.code}（端点可达）")
        except Exception as e:
            print(f"[preflight] FAIL {m} ({base}): {e}")
            ok = False
    return ok


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
                    "kind": classify_tool_error(ev.get("tool_err") or ""),
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
        "tool_failures": sum(1 for e in tool_errors if e.get("kind") != "verify"),
        "verify_failures": sum(1 for e in tool_errors if e.get("kind") == "verify"),
        "first_error_step": first_error_step,
        "turns_complete": turns_complete,
        "final_err": final_err,
    }


def execute_task(cmd, cwd, timeout):
    """运行一次 bounty 子进程；返回 (exit_code, timeout_flag, stdout, stderr)。"""
    try:
        r = subprocess.run(
            cmd, cwd=str(cwd), timeout=timeout,
            capture_output=True, env=os.environ.copy(),
        )
        return (r.returncode, False,
                r.stdout.decode("utf-8", errors="replace"),
                r.stderr.decode("utf-8", errors="replace"))
    except subprocess.TimeoutExpired as e:
        out = (e.stdout or b"").decode("utf-8", errors="replace")
        err = (e.stderr or b"").decode("utf-8", errors="replace")
        return None, True, out, err


def run_one(args, task, model, run_id, bounty_bin):
    slug = model_slug(model)
    task_dir = args.work / run_id / slug / task["id"]
    src = task_dir / "src"
    pristine = task_dir / "pristine"
    run_json = task_dir / "run.json"
    if run_json.exists() and not args.redo:
        print(f"[skip] {model} {task['id']} (already ran)")
        return task["id"], model, None

    retries = 0
    while True:
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
        for img in task.get("images") or []:
            img_path = EVAL_DIR / img
            cmd.append("--image=" + str(img_path))
        started = time.time()
        exit_code, timeout, stdout, stderr = execute_task(cmd, src, args.timeout)
        wall = round(time.time() - started, 1)

        # P7-1：网络/DNS/服务端瞬断导致的失败自动重试（模型行为失败不重试）
        retryable = (exit_code != 0 and not timeout
                     and retries < args.max_retries
                     and is_network_failure(stderr, stdout))
        if retryable:
            retries += 1
            print(f"[retry] {model} {task['id']} attempt={retries} "
                  f"reason={stderr.strip()[:120] or stdout.strip()[:120]}")
            continue
        break

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
        "retried": retries,
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


def warn_non_ascii_workdir(work_dir):
    """中文/非 ASCII 路径在 cmd 下更易触发编码失败（P6 残余失败第二大来源）。"""
    if any(ord(c) > 127 for c in str(work_dir)):
        print(f"[WARN] work 路径含非 ASCII 字符：{work_dir}")
        print("       建议 --work 指定纯 ASCII 路径（如 C:\\bounty-eval\\work）以规避 cmd 编码问题")


def find_failed_tasks(work_dir, run_id, models):
    """扫描 <work>/<run_id>/<slug>/*/run.json，返回运行级失败任务 id 集合。"""
    failed = set()
    for slug in {model_slug(m) for m in models}:
        base = work_dir / run_id / slug
        if not base.exists():
            continue
        for rj in sorted(base.glob("*/run.json")):
            try:
                d = load_json(rj)
            except Exception:
                continue
            if d.get("exit_code") not in (0, None) or d.get("timeout"):
                failed.add(d.get("task_id"))
    return failed


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
    ap.add_argument("--no-health-check", action="store_true",
                    help="skip the API endpoint preflight probe")
    ap.add_argument("--max-retries", type=int, default=2,
                    help="auto-retry count for network/DNS failures (default 2)")
    ap.add_argument("--redo-failed", type=str, default=None, metavar="RUN_ID",
                    help="re-run tasks that failed (exit!=0 or timeout) in an earlier run dir")
    args = ap.parse_args()

    data = load_json(args.tasks)
    tasks = data["tasks"]
    models = [m.strip() for m in args.models.split(",") if m.strip()]
    ids = [i.strip() for i in args.task_ids.split(",") if i.strip()]
    if ids:
        tasks = [t for t in tasks if t["id"] in ids]

    if args.redo_failed:
        # 从旧 run 目录挑出运行级失败任务（exit!=0 或 timeout），映射回 tasks.json
        failed_ids = find_failed_tasks(args.work, args.redo_failed, models)
        if not failed_ids:
            sys.exit(f"redo-failed: {args.redo_failed} 下没有运行级失败任务")
        tasks = [t for t in tasks if t["id"] in failed_ids]
        print(f"[redo-failed] {args.redo_failed} -> 重跑 {len(tasks)} 个失败任务: {sorted(failed_ids)}")

    if not tasks:
        sys.exit("no tasks selected")

    if not args.no_health_check:
        if not preflight_models(args.config, models):
            sys.exit("preflight FAIL: 模型端点不可达。确认网络/代理后重试，或用 --no-health-check 跳过。")

    bounty_bin = ensure_binary(REPO_ROOT, force=args.rebuild)
    args.work.mkdir(parents=True, exist_ok=True)
    warn_non_ascii_workdir(args.work)
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
