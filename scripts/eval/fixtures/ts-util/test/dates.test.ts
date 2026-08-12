import test from "node:test";
import assert from "node:assert/strict";
import { daysBetween, formatDate } from "../src/dates.ts";

test("formatDate zero pads", () => {
  assert.equal(formatDate(new Date(2026, 0, 5)), "2026-01-05");
});

test("daysBetween counts calendar days", () => {
  assert.equal(daysBetween(new Date(2026, 0, 1), new Date(2026, 0, 4)), 3);
});
