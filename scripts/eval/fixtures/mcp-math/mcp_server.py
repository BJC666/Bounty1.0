# -*- coding: utf-8 -*-
"""最小 MCP stdio 服务器：math（add / calc_fib），供 Eval E 类任务使用。"""
import json
import sys


def fib(n):
    a, b = 0, 1
    for _ in range(max(int(n), 0)):
        a, b = b, a + b
    return a


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue
        mid = req.get("id")
        method = req.get("method")
        result = None
        if method == "initialize":
            result = {"protocolVersion": "2024-11-05", "capabilities": {},
                      "serverInfo": {"name": "math", "version": "1.0"}}
        elif method == "tools/list":
            result = {"tools": [
                {"name": "add", "description": "两个整数相加",
                 "inputSchema": {"type": "object",
                                 "properties": {"a": {"type": "integer"}, "b": {"type": "integer"}},
                                 "required": ["a", "b"]}},
                {"name": "calc_fib", "description": "计算斐波那契第 n 项（F(0)=0）",
                 "inputSchema": {"type": "object",
                                 "properties": {"n": {"type": "integer"}},
                                 "required": ["n"]}},
            ]}
        elif method == "tools/call":
            params = req.get("params") or {}
            name = params.get("name")
            args = params.get("arguments") or {}
            if name == "add":
                text = str(int(args.get("a", 0)) + int(args.get("b", 0)))
            elif name == "calc_fib":
                text = str(fib(args.get("n", 0)))
            else:
                text = "unknown tool"
            result = {"content": [{"type": "text", "text": text}]}
        elif method == "resources/list":
            result = {"resources": []}
        elif method == "prompts/list":
            result = {"prompts": []}
        if mid is not None:
            sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": mid, "result": result}, ensure_ascii=False) + "\n")
            sys.stdout.flush()


if __name__ == "__main__":
    main()
