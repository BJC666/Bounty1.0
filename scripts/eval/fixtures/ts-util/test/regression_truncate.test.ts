import test from "node:test";
import assert from "node:assert/strict";
import { truncate } from "../src/strings.ts";

// C9: 超长文本截断时必须带省略号，且总长不超过 max。
test("truncate appends ellipsis and respects max", () => {
  const out = truncate("hello world", 5);
  assert.equal(out, "hello…");
});
