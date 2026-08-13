# -*- coding: utf-8 -*-
"""P7-1 健壮性回归测试：网络失败判定 / 错误口径分列 / 自动重试 / 健康预检 / redo-failed。

零依赖（unittest + mock），运行：
    python -m unittest scripts.eval.test_runner_robustness -v
或直接：  python scripts/eval/test_runner_robustness.py
"""
import io as _io
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import runner  # noqa: E402


class FakeCompleted:
    def __init__(self, rc=0, out=b"", err=b""):
        self.returncode = rc
        self.stdout = out
        self.stderr = err


class TestNetworkFailure(unittest.TestCase):
    def test_dns_failure_detected(self):
        self.assertTrue(runner.is_network_failure(
            "Error: network error: dial tcp: lookup token-plan...: no such host", ""))
        self.assertTrue(runner.is_network_failure("", "dial tcp ... connection refused"))

    def test_model_behavior_failure_not_retried(self):
        self.assertFalse(runner.is_network_failure("go build failed: undefined: x", ""))
        self.assertFalse(runner.is_network_failure("", ""))


class TestClassifyToolError(unittest.TestCase):
    def test_verify_test_failure(self):
        e = "【错误类型】其他错误\n【原因】--- FAIL: TestPendingExcludesDone (0.00s)\nexit status 1"
        self.assertEqual(runner.classify_tool_error(e), "verify")

    def test_verify_command_nonzero(self):
        e = "=== RUN TestDoneMarksExactID\n--- PASS ...\nexit status 1"
        self.assertEqual(runner.classify_tool_error(e), "verify")

    def test_tool_file_not_found(self):
        e = "【错误类型】文件不存在\n【原因】open D:/x/src/a.ts: The system cannot find the file specified."
        self.assertEqual(runner.classify_tool_error(e), "tool")

    def test_tool_encoding(self):
        e = "【错误类型】路径/编码错误\n【原因】'tail' 不是内部或外部命令\nexit status 255"
        self.assertEqual(runner.classify_tool_error(e), "tool")

    def test_tool_cmd_not_found(self):
        e = "系统找不到指定的文件。\r\nexit status 1"
        self.assertEqual(runner.classify_tool_error(e), "tool")


class TestParseTranscriptKinds(unittest.TestCase):
    def test_split_tool_and_verify(self):
        lines = [
            {"type": "step"},
            {"type": "tool_call", "tool_name": "read_file"},
            {"type": "tool_result", "tool_name": "read_file",
             "tool_err": "【错误类型】文件不存在\nopen no such file"},
            {"type": "tool_call", "tool_name": "bash"},
            {"type": "tool_result", "tool_name": "bash",
             "tool_err": "--- FAIL: TestX\nexit status 1"},
        ]
        m = runner.parse_transcript("\n".join(json.dumps(x) for x in lines))
        self.assertEqual(m["n_tool_errors"], 2)
        self.assertEqual(m["tool_failures"], 1)
        self.assertEqual(m["verify_failures"], 1)
        self.assertEqual(m["tool_errors"][0]["kind"], "tool")
        self.assertEqual(m["tool_errors"][1]["kind"], "verify")


class TestRetryLoop(unittest.TestCase):
    def _make_args(self, tmp):
        return SimpleNamespace(
            work=Path(tmp), config=Path(runner.EVAL_DIR) / "config" / "bounty.toml",
            timeout=60, max_retries=2, max_steps=50, redo=False,
        )

    def test_retries_on_dns_failure_then_succeeds(self):
        tmp = tempfile.mkdtemp(prefix="eval_robust_")
        tasks = runner.load_json(runner.EVAL_DIR / "tasks.json")["tasks"]
        task = next(t for t in tasks if t["id"] == "A1")
        calls = {"n": 0}

        def fake_run(cmd, cwd, timeout, capture_output, env):
            calls["n"] += 1
            if calls["n"] == 1:
                return FakeCompleted(rc=1, err=b"Error: step 0: network error: no such host")
            return FakeCompleted(rc=0, out=b'{"type":"step"}\n{"type":"usage","input_tokens":1}\n')

        with mock.patch.object(runner.subprocess, "run", side_effect=fake_run):
            tid, model, result = runner.run_one(
                self._make_args(tmp), task, "qwen/qwen3.8-max", "test-run-1", "bounty.exe")

        self.assertEqual(tid, "A1")
        self.assertIsNotNone(result)
        self.assertEqual(result["retried"], 1)
        self.assertEqual(result["exit_code"], 0)

    def test_no_retry_on_model_failure(self):
        tmp = tempfile.mkdtemp(prefix="eval_robust_")
        tasks = runner.load_json(runner.EVAL_DIR / "tasks.json")["tasks"]
        task = next(t for t in tasks if t["id"] == "A1")
        calls = {"n": 0}

        def fake_run(cmd, cwd, timeout, capture_output, env):
            calls["n"] += 1
            return FakeCompleted(rc=1, err=b"go build failed: undefined: x")

        with mock.patch.object(runner.subprocess, "run", side_effect=fake_run):
            _, _, result = runner.run_one(
                self._make_args(tmp), task, "qwen/qwen3.8-max", "test-run-2", "bounty.exe")

        self.assertEqual(result["retried"], 0)
        self.assertEqual(result["exit_code"], 1)
        self.assertEqual(calls["n"], 1)


class TestPreflight(unittest.TestCase):
    def test_401_is_reachable(self):
        cfg = Path(runner.EVAL_DIR) / "config" / "bounty.toml"
        with mock.patch.object(runner.urllib.request, "urlopen",
                               side_effect=runner.urllib.error.HTTPError(
                                   "http://x/models", 401, "Unauthorized", None, None)):
            self.assertTrue(runner.preflight_models(cfg, ["qwen/qwen3.8-max"]))

    def test_network_error_fails(self):
        cfg = Path(runner.EVAL_DIR) / "config" / "bounty.toml"
        with mock.patch.object(runner.urllib.request, "urlopen",
                               side_effect=OSError("no such host")):
            self.assertFalse(runner.preflight_models(cfg, ["qwen/qwen3.8-max"]))


class TestFindFailedTasks(unittest.TestCase):
    def test_collects_exit_and_timeout(self):
        tmp = Path(tempfile.mkdtemp(prefix="eval_robust_"))
        base = tmp / "run-x" / "qwen__qwen3.8-max"
        (base / "A1").mkdir(parents=True)
        (base / "A2").mkdir(parents=True)
        (base / "A3").mkdir(parents=True)
        runner.save_json(base / "A1" / "run.json",
                         {"task_id": "A1", "exit_code": 0, "timeout": False})
        runner.save_json(base / "A2" / "run.json",
                         {"task_id": "A2", "exit_code": 1, "timeout": False})
        runner.save_json(base / "A3" / "run.json",
                         {"task_id": "A3", "exit_code": 0, "timeout": True})
        got = runner.find_failed_tasks(tmp, "run-x", ["qwen/qwen3.8-max"])
        self.assertEqual(got, {"A2", "A3"})


if __name__ == "__main__":
    unittest.main(verbosity=2)
