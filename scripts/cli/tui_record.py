# -*- coding: utf-8 -*-
"""P7-4c real TTY recording: drive `bounty chat` under winpty, three scenarios.

S1 navigation: up/down/pgup/pgdn/home/end scrolling
S2 collapse: e expands latest tool call, E expands/collapses all
S3 diff: edit_file tool call expanded showing colored diff

Usage: python scripts/cli/tui_record.py
Output: docs/eval/p7-4-tui/transcript.log (full ANSI transcript)
"""
import pathlib
import shutil
import sys
import threading
import time

import winpty

ROOT = pathlib.Path(__file__).resolve().parents[2]
BOUNTY_EXE = ROOT / "scripts" / "eval" / "bin" / "bounty.exe"
SCRATCH = pathlib.Path("C:/bounty-eval/tui-demo")
OUT_DIR = ROOT / "docs" / "eval" / "p7-4-tui"
FIXTURE = ROOT / "scripts" / "eval" / "fixtures" / "go-todo"

NL = chr(10)

CONFIG_TOML = (
    "# P7-4c TUI recording config" + NL +
    "config_version = 1" + NL +
    'default_model = "qwen/qwen3.8-max"' + NL +
    'language = "zh"' + NL + NL +
    "[[providers]]" + NL +
    'name = "qwen"' + NL +
    'kind = "openai"' + NL +
    'base_url = "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode"' + NL +
    'models = ["qwen3.8-max"]' + NL +
    'api_key_env = "QWEN_TOKENPLAN_API_KEY"' + NL +
    "context_window = 128000" + NL + NL +
    "[agent]" + NL +
    "temperature = 0.0" + NL +
    "max_steps = 30" + NL + NL +
    "[permissions.allow]" + NL +
    'tools = ["glob", "read_file", "grep", "edit_file", "write_file", "bash"]' + NL +
    'bash_patterns = ["*"]' + NL + NL +
    "[devet]" + NL +
    "enabled = false" + NL + NL +
    "[remote]" + NL +
    "enabled = false" + NL
)

ESC = chr(27)
CTRL_C = chr(3)
ENTER = chr(13)
KEYS = {
    "up": ESC + "[A", "down": ESC + "[B", "pgup": ESC + "[5~", "pgdn": ESC + "[6~",
    "home": ESC + "[H", "end": ESC + "[F", "esc": ESC,
    "enter": ENTER, "e": "e", "E": "E",
}


class Recorder:
    def __init__(self, proc):
        self.proc = proc
        self.buf = ""
        self.lock = threading.Lock()
        self.alive = True
        self.thread = threading.Thread(target=self._read, daemon=True)
        self.thread.start()

    def _read(self):
        try:
            while self.alive:
                data = self.proc.read(4096)
                if not data:
                    break
                with self.lock:
                    self.buf += data
        except Exception:
            pass

    def snapshot(self):
        with self.lock:
            return self.buf

    def clear(self):
        with self.lock:
            self.buf = ""

    def wait_stable(self, quiet_for=6.0, max_wait=150.0):
        last = len(self.snapshot())
        start = time.time()
        quiet = 0.0
        while time.time() - start < max_wait:
            time.sleep(1.0)
            cur = len(self.snapshot())
            if cur == last:
                quiet += 1.0
                if quiet >= quiet_for:
                    return True
            else:
                quiet = 0.0
                last = cur
        return False

    def send(self, keys):
        if isinstance(keys, bytes):
            keys = keys.decode("utf-8", errors="replace")
        parts = keys.split(ENTER)
        for i, part in enumerate(parts):
            if part:
                for j in range(0, len(part), 12):
                    self.proc.write(part[j:j + 12])
                    time.sleep(0.06)
            if i < len(parts) - 1:
                self.proc.write(ENTER)
                time.sleep(0.3)
        time.sleep(0.4)


def decode(data: str) -> str:
    return data

def main() -> int:
    if not BOUNTY_EXE.exists():
        print("missing bounty.exe, run: go build -o scripts/eval/bin/bounty.exe ./cmd/bounty")
        return 1

    if SCRATCH.exists():
        shutil.rmtree(str(SCRATCH))
    SCRATCH.mkdir(parents=True)
    (SCRATCH / "bounty.toml").write_text(CONFIG_TOML, encoding="utf-8")
    repo = SCRATCH / "repo"
    shutil.copytree(str(FIXTURE), str(repo), ignore=shutil.ignore_patterns(".bounty"))
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    transcript_path = OUT_DIR / "transcript.log"

    log = []

    def mark(tag: str):
        log.append(NL + "=" * 70 + NL + tag + NL + "=" * 70 + NL)

    print("[tui] spawning bounty chat under winpty ...")
    proc = winpty.PtyProcess.spawn(
        [str(BOUNTY_EXE), "chat"], cwd=str(SCRATCH), dimensions=(36, 110)
    )
    rec = Recorder(proc)
    time.sleep(6.0)
    mark("S0 startup + /status")
    rec.send("/status" + KEYS["enter"])
    time.sleep(2.5)
    log.append(decode(rec.snapshot()))

    mark("S1 navigation: send glob research message")
    rec.clear()
    rec.send("use glob to list all files in the repo, then read README.md and repeat its full content" + KEYS["enter"])
    rec.wait_stable(quiet_for=6.0, max_wait=150.0)
    log.append(decode(rec.snapshot()))

    mark("S1 navigation: up/down")
    rec.clear()
    for _ in range(3):
        rec.send(KEYS["up"])
        time.sleep(0.3)
    for _ in range(3):
        rec.send(KEYS["down"])
        time.sleep(0.3)
    time.sleep(1.0)
    log.append(decode(rec.snapshot()))

    mark("S1 navigation: pgup/pgdn/home/end")
    rec.clear()
    rec.send(KEYS["pgup"]); time.sleep(0.5)
    rec.send(KEYS["pgdn"]); time.sleep(0.5)
    rec.send(KEYS["home"]); time.sleep(0.5)
    rec.send(KEYS["end"]); time.sleep(0.5)
    time.sleep(1.0)
    log.append(decode(rec.snapshot()))

    mark("S2 collapse: e expand latest tool call")
    rec.clear()
    rec.send(KEYS["esc"])
    time.sleep(0.5)
    rec.send(KEYS["e"])
    time.sleep(2.0)
    log.append(decode(rec.snapshot()))

    mark("S2 collapse: E expand all tool calls")
    rec.clear()
    rec.send(KEYS["esc"])
    time.sleep(0.5)
    rec.send(KEYS["E"])
    time.sleep(2.0)
    log.append(decode(rec.snapshot()))

    mark("S2 collapse: E again collapse all")
    rec.clear()
    rec.send(KEYS["esc"])
    time.sleep(0.5)
    rec.send(KEYS["E"])
    time.sleep(2.0)
    log.append(decode(rec.snapshot()))

    mark("S3 diff: send edit_file task")
    rec.clear()
    rec.send("use edit_file to append one line to README.md: <!-- tui-demo diff -->" + KEYS["enter"])
    rec.wait_stable(quiet_for=6.0, max_wait=150.0)
    log.append(decode(rec.snapshot()))

    mark("S3 diff: e expand edit_file tool call (diff view)")
    rec.clear()
    rec.send(KEYS["esc"])
    time.sleep(0.5)
    rec.send(KEYS["e"])
    time.sleep(1.5)
    rec.send(KEYS["home"])   # scroll to top of output where tool panels live
    time.sleep(1.5)
    log.append(decode(rec.snapshot()))

    mark("S9 exit Ctrl+C")
    rec.clear()
    rec.send(CTRL_C)
    time.sleep(2.0)
    log.append(decode(rec.snapshot()))
    proc.terminate(force=True)

    full = "".join(log)
    transcript_path.write_text(full, encoding="utf-8")
    print("[tui] transcript saved:", transcript_path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
