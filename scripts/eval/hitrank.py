#!/usr/bin/env python3
"""P8-2 repomap 命中率分析：从一次 eval run 的 transcript 统计各 fixture
实际读取的文件序列，输出 `.bounty/repomap-boost.json`（高频文件提前渲染）。

用法：
  python scripts/eval/hitrank.py --runs <run_id>[,<run_id>...] [--work DIR] [--dry]
默认 work 目录 = scripts/eval/work；也支持 C:\\bounty-eval\\work 等外部目录。

排序规则（杠杆=首步定位）：
  - 每任务首次 read_file/glob/grep/code_index 命中的文件权重 3
  - 后续命中的文件权重 1
  - 同分按路径字典序，保证确定性
只统计任务实际运行过的 fixture；写入 fixtures/<fixture>/.bounty/repomap-boost.json。
"""

import argparse
import collections
import json
import os
import shutil
import sys

EVAL_DIR = os.path.dirname(os.path.abspath(__file__))
FIXTURES = os.path.join(EVAL_DIR, "fixtures")

TOOL_PATH_KEYS = {
    "read_file": ("file_path",),
    "edit_file": ("file_path",),
    "write_file": ("file_path",),
    "glob": ("path",),
    "grep": ("path",),
    "code_index": ("path",),
}

FIRST_WEIGHT = 3
LATER_WEIGHT = 1


def parse_transcript(path):
    calls = []
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    ev = json.loads(line)
                except Exception:
                    continue
                if ev.get("type") != "tool_call":
                    continue
                name = ev.get("tool_name") or ""
                if name not in TOOL_PATH_KEYS:
                    continue
                args = ev.get("tool_args") or {}
                p = ""
                for key in TOOL_PATH_KEYS[name]:
                    p = args.get(key) or ""
                    if p:
                        break
                calls.append((name, p))
    except OSError:
        return None
    return calls


def analyze_run(run_dir):
    """Returns {fixture: {relpath: score}} and {fixture: task_count}."""
    scores = collections.defaultdict(collections.Counter)
    counts = collections.Counter()
    if not os.path.isdir(run_dir):
        print(f"[warn] run dir missing: {run_dir}", file=sys.stderr)
        return scores, counts
    for model in sorted(os.listdir(run_dir)):
        model_dir = os.path.join(run_dir, model)
        if not os.path.isdir(model_dir):
            continue
        for task in sorted(os.listdir(model_dir)):
            task_dir = os.path.join(model_dir, task)
            judge_path = os.path.join(task_dir, "judge.json")
            trans_path = os.path.join(task_dir, "transcript.jsonl")
            if not (os.path.isfile(judge_path) and os.path.isfile(trans_path)):
                continue
            try:
                with open(judge_path, encoding="utf-8") as f:
                    judge = json.load(f)
            except Exception:
                continue
            fixture = judge.get("fixture")
            if not fixture:
                continue
            src_root = os.path.join(task_dir, "src")
            calls = parse_transcript(trans_path)
            if calls is None:
                continue
            counts[fixture] += 1
            seen = set()
            for idx, (name, p) in enumerate(calls):
                if not p:
                    continue
                abs_p = os.path.normpath(p)
                if not os.path.isabs(abs_p):
                    # P8-3 之后模型按提示使用 workspace-relative 路径，
                    # 相对路径要拼到任务 src 根再解析。
                    abs_p = os.path.join(src_root, abs_p)
                if os.name == "nt":
                    abs_p = abs_p.replace("/", os.sep)
                try:
                    rel = os.path.relpath(abs_p, src_root)
                except ValueError:
                    continue
                if rel.startswith("..") or os.path.isabs(rel):
                    continue
                rel = rel.replace(os.sep, "/")
                if not os.path.isfile(os.path.join(src_root, *rel.split("/"))):
                    continue
                if rel in seen:
                    continue
                seen.add(rel)
                scores[fixture][rel] += FIRST_WEIGHT if idx == 0 else LATER_WEIGHT
    return scores, counts


def write_boost(fixture, score_counter, dry=False):
    fixture_dir = os.path.join(FIXTURES, fixture)
    if not os.path.isdir(fixture_dir):
        print(f"[skip] unknown fixture {fixture}", file=sys.stderr)
        return None
    ordered = [p for p, _ in score_counter.most_common()]
    ordered.sort(key=lambda p: (-score_counter[p], p))
    payload = {"order": ordered}
    out_dir = os.path.join(fixture_dir, ".bounty")
    out_path = os.path.join(out_dir, "repomap-boost.json")
    if dry:
        print(f"[dry] {fixture}: {ordered}")
        return None
    os.makedirs(out_dir, exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
    print(f"[write] {out_path} ({len(ordered)} files)")
    return out_path


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs", required=True, help="run ids, comma separated")
    ap.add_argument("--work", default=os.path.join(EVAL_DIR, "work"), help="eval work root")
    ap.add_argument("--dry", action="store_true", help="print order without writing")
    args = ap.parse_args()

    agg = collections.defaultdict(collections.Counter)
    counts = collections.Counter()
    for rid in [r.strip() for r in args.runs.split(",") if r.strip()]:
        s, c = analyze_run(os.path.join(args.work, rid))
        for fixture, counter in s.items():
            agg[fixture] += counter
            counts[fixture] += c[fixture]

    if not agg:
        print("[error] no transcripts matched", file=sys.stderr)
        return 1
    for fixture in sorted(agg):
        print(f"[hit] {fixture}: {counts[fixture]} tasks, {len(agg[fixture])} distinct files")
        write_boost(fixture, agg[fixture], dry=args.dry)
    return 0


if __name__ == "__main__":
    sys.exit(main())
