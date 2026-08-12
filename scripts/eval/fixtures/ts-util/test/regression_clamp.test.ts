import test from "node:test";
import assert from "node:assert/strict";
import { clamp } from "../src/numbers.ts";

// C10: clamp 低于下界时必须返回下界。
test("clamp returns min when below range", () => {
  assert.equal(clamp(3, 5, 10), 5);
});

test("clamp returns max when above range", () => {
  assert.equal(clamp(12, 5, 10), 10);
});
