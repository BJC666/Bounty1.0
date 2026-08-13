# -*- coding: utf-8 -*-
"""P7-4a 一键验证：启动官方 SDK SSE server -> realcheck 连接 3 个真实 MCP server -> 存转写。

用法: python scripts/mcp/run_realcheck.py [--out docs/eval/p7-4-mcp-transcript.txt]
"""
import argparse
import pathlib
import subprocess
import sys
import time
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[2]
SSE_SCRIPT = ROOT / "scripts" / "mcp" / "sse_math_server.py"
CONFIG = ROOT / "scripts" / "mcp" / "realcheck_config.json"


def wait_sse(port: int, tries: int = 20) -> bool:
    for _ in range(tries):
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/sse", timeout=2):
                return True
        except Exception:
            time.sleep(0.5)
    return False


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=str(ROOT / "docs" / "eval" / "p7-4-mcp-transcript.txt"))
    args = ap.parse_args()

    sse = subprocess.Popen(
        [sys.executable, str(SSE_SCRIPT), "18080"],
        cwd=str(ROOT),
        creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
    )
    try:
        if not wait_sse(18080):
            print("FAIL: SSE server did not start")
            return 1
        print("[sse] math-sse server up (pid=%d)" % sse.pid)
        out = pathlib.Path(args.out)
        out.parent.mkdir(parents=True, exist_ok=True)
        r = subprocess.run(
            ["go", "run", "./scripts/mcp/realcheck", str(CONFIG), str(out)],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
        print(r.stdout)
        if r.stderr:
            print("STDERR:", r.stderr[-2000:])
        return r.returncode
    finally:
        sse.terminate()
        try:
            sse.wait(timeout=5)
        except subprocess.TimeoutExpired:
            sse.kill()


if __name__ == "__main__":
    sys.exit(main())
