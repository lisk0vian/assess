// Small check for the reading ranges. Run with:
//   node --test web/scripts/reading-parts.test.mjs
import assert from "node:assert/strict";
import test from "node:test";

import { MAX_PART, MIN_PART, OVERLAP, partAtPoint, pauseFor, rectsToPath, splitParts, speedOf, timingsOf } from "../src/components/reading.ts";

const seq = (n) => Array.from({ length: n }, (_, i) => `w${i}`);
const rnd = (seed) => {
  let n = seed >>> 0;
  return () => {
    n = (n * 1664525 + 1013904223) >>> 0;
    return n / 2 ** 32;
  };
};

function check(n, seed) {
  const words = seq(n);
  const parts = splitParts(words, [], rnd(seed));
  if (n === 0) return assert.deepEqual(parts, []);
  assert.ok(parts.length >= 1);
  assert.equal(parts[0].start, 0);
  assert.equal(parts.at(-1).end, n);
  for (let i = 0; i < parts.length; i++) {
    assert.ok(parts[i].end > parts[i].start);
    if (i) assert.equal(parts[i].start, Math.max(0, parts[i - 1].end - OVERLAP), `missing context before part ${i}`);
    if (i < parts.length - 1) {
      const fresh = parts[i].end - (i ? parts[i - 1].end : 0);
      assert.ok(fresh >= MIN_PART, `short part ${i}`);
      assert.ok(fresh <= MAX_PART, `long part ${i}`);
    }
  }
  if (n > 5) assert.ok(parts.length >= 2, "a prompt over five words must be split");
  return parts;
}

test("overlapping ranges cover every word with context", () => {
  for (const n of [0, 1, 3, 5, 6, 7, 8, 9, 10, 12, 15, 16, 20, 40, 100, 137]) {
    for (let seed = 1; seed <= 20; seed++) check(n, seed);
  }
});

test("short prompts stay whole", () => {
  for (const n of [1, 3, 5]) assert.equal(check(n, 1).length, 1);
});

test("a natural pause wins when it fits", () => {
  const words = seq(100);
  words[13] = "cierre.";
  const parts = splitParts(words, [], () => 0.5);
  assert.equal(parts[0].end, 14);
});

test("paragraph breaks win", () => {
  const words = seq(60);
  const breaks = words.map(() => false);
  breaks[11] = true;
  const parts = splitParts(words, breaks, () => 0.5);
  assert.equal(parts[0].end, 11);
});

test("speeds and pauses fall back to normal", () => {
  assert.equal(speedOf("fast"), "fast");
  assert.equal(speedOf("nonsense"), "normal");
  assert.equal(timingsOf("slow").wordMs, 340);
  assert.equal(timingsOf("normal").wordMs, 260);
  const words = ["uno,", "dos.", "tres"];
  const breaks = [false, false, true];
  const part = { start: 0, end: 2 };
  assert.equal(pauseFor(words, breaks, part, timingsOf("normal")), 60 + 300 + 450);
});

test("rectsToPath builds a clamped disjoint path", () => {
  const path = rectsToPath([{ left: 10, top: 20, width: 30, height: 10 }], 0, { width: 38, height: 60 });
  assert.equal(path, "M10 20H38V30H10Z");
  assert.equal(rectsToPath([], 0), "");
});

test("partAtPoint maps a tap to the containing word's part", () => {
  const words = [
    { rects: [{ left: 0, top: 0, width: 10, height: 10 }] },
    { rects: [{ left: 12, top: 0, width: 10, height: 10 }] },
    { rects: [{ left: 0, top: 20, width: 10, height: 10 }] },
  ];
  // Word 1 is repeated at the start of the second range.
  const parts = [{ start: 0, end: 2 }, { start: 1, end: 3 }];
  assert.equal(partAtPoint(words, parts, 3, 3), 0);
  assert.equal(partAtPoint(words, parts, 13, 3), 1);
  assert.equal(partAtPoint(words, parts, 3, 23), 1);
  // A tap in the padding still lands on the nearest range.
  assert.equal(partAtPoint(words, parts, 40, 4), 1);
  assert.equal(partAtPoint([], [], 0, 0), -1);
});
