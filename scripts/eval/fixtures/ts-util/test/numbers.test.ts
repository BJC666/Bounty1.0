import test from "node:test";
import assert from "node:assert/strict";
import { formatPercent, sum } from "../src/numbers.ts";

test("formatPercent formats with digits", () => {
  assert.equal(formatPercent(0.5, 2), "50.00%");
});

test("sum adds values", () => {
  assert.equal(sum([1, 2, 3]), 6);
});
