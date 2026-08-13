# -*- coding: utf-8 -*-
"""P7-4a 真实 MCP SSE server：官方 Python SDK（FastMCP）实现 math（add / calc_fib）。

用法: python sse_math_server.py [port]   # 默认 18080，SSE 端点 http://127.0.0.1:<port>/sse
"""
import sys

from mcp.server.fastmcp import FastMCP

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18080

mcp = FastMCP("math-sse", host="127.0.0.1", port=PORT)


@mcp.tool()
def add(a: int, b: int) -> int:
    """两个整数相加"""
    return a + b


@mcp.tool()
def calc_fib(n: int) -> int:
    """计算斐波那契第 n 项（F(0)=0）"""
    a, b = 0, 1
    for _ in range(max(int(n), 0)):
        a, b = b, a + b
    return a


if __name__ == "__main__":
    mcp.run(transport="sse")
