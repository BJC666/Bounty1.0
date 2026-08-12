import test from "node:test";
import assert from "node:assert/strict";
import { camelCase, countWords } from "../src/strings.ts";

test("camelCase joins words", () => {
  assert.equal(camelCase("hello world"), "helloWorld");
});

test("countWords handles empty input", () => {
  assert.equal(countWords("   "), 0);
});
