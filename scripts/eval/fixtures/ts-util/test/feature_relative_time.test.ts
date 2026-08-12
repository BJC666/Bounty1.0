import test from "node:test";
import assert from "node:assert/strict";
import { relativeTime } from "../src/relative_time.ts";

// B10: 把秒数渲染为中文相对时间。
test("relative time for seconds", () => {
  assert.equal(relativeTime(45), "45 秒前");
});

test("relative time for minutes", () => {
  assert.equal(relativeTime(300), "5 分钟前");
});
