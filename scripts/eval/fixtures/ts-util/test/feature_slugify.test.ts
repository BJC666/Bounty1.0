import test from "node:test";
import assert from "node:assert/strict";
import { slugify } from "../src/slugify.ts";

// B8: 把文本转成 URL slug。
test("slugify lowercases and replaces spaces", () => {
  assert.equal(slugify("Hello World"), "hello-world");
});

test("slugify strips punctuation", () => {
  assert.equal(slugify("Café, 東京!"), "café-東京");
});
