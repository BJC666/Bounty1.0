import test from "node:test";
import assert from "node:assert/strict";
import { median } from "../src/median.ts";

// B9: 中位数。
test("median of odd count", () => {
  assert.equal(median([3, 1, 2]), 2);
});

test("median of even count", () => {
  assert.equal(median([1, 2, 3, 4]), 2.5);
});
