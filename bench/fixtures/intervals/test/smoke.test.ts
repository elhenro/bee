// smoke tests: basic sanity for mergeIntervals. run them with:
//   sh run-tests.sh
// these cover the obvious cases only - read the full JSDoc contract in
// src/intervals.ts for every requirement you must satisfy.

import { test } from "node:test";
import assert from "node:assert/strict";
import { mergeIntervals } from "../src/intervals.ts";

test("empty input returns empty", () => {
  assert.deepEqual(mergeIntervals([]), []);
});

test("single interval is unchanged", () => {
  assert.deepEqual(mergeIntervals([[1, 3]]), [[1, 3]]);
});

test("overlapping intervals merge", () => {
  assert.deepEqual(mergeIntervals([[1, 3], [2, 6]]), [[1, 6]]);
});

test("disjoint intervals stay separate", () => {
  assert.deepEqual(mergeIntervals([[1, 2], [5, 7]]), [[1, 2], [5, 7]]);
});
