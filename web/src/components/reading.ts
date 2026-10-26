// Pure helpers for the prompt reading guide: contiguous word ranges, their
// pace and the clip path that reveals them. No DOM here, so they can be
// checked without a browser.

export interface Rect {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface Part {
  start: number; // first word, included
  end: number; // last word, excluded
}

export type ReadSpeed = "slow" | "normal" | "fast";

export interface Timings {
  wordMs: number;
  pauseMs: number;
  sentenceMs: number;
  paragraphMs: number;
}

// wordMs follows a readable pace (about 175 / 230 / 335 words per minute).
const SPEEDS: Record<ReadSpeed, Timings> = {
  slow: { wordMs: 340, pauseMs: 80, sentenceMs: 400, paragraphMs: 600 },
  normal: { wordMs: 260, pauseMs: 60, sentenceMs: 300, paragraphMs: 450 },
  fast: { wordMs: 180, pauseMs: 40, sentenceMs: 200, paragraphMs: 300 },
};

export const MIN_PART = 4;
export const MAX_PART = 15;
/** Words repeated from the previous range so the next one starts with context. */
export const OVERLAP = 2;
const TARGET_SHARE = 0.18;

/** speedOf accepts any server string and falls back to normal. */
export function speedOf(raw: string | undefined): ReadSpeed {
  return raw === "slow" || raw === "fast" ? raw : "normal";
}

export function timingsOf(raw: string | undefined): Timings {
  return SPEEDS[speedOf(raw)];
}

const SENTENCE = /[.!?…]["'»”)]*$/;
const CLAUSE = /[,;:]["'»”)]*$/;

function boundary(words: readonly string[], breaks: readonly boolean[], pos: number): number {
  if (breaks[pos]) return 3; // a new paragraph or block
  const last = words[pos - 1] ?? "";
  if (SENTENCE.test(last)) return 2;
  if (CLAUSE.test(last)) return 1;
  return 0;
}

/**
 * splitParts divides n words into ranges. Each range after the first repeats
 * the last OVERLAP words of the previous one, so the student sees the context
 * instead of having to remember the previous range. Fresh words are about
 * 18 % of the prompt, with a little randomness, and prefer a natural pause
 * near the target. The final part may be shorter than MIN_PART; no other part
 * is.
 */
export function splitParts(
  words: readonly string[],
  breaks: readonly boolean[] = [],
  random: () => number = Math.random,
): Part[] {
  const n = words.length;
  if (n === 0) return [];
  if (n <= 5) return [{ start: 0, end: n }];

  const target = Math.max(MIN_PART, Math.min(MAX_PART, Math.round(n * TARGET_SHARE)));
  let count = Math.max(2, Math.round(n / target));
  while (count > 2 && n / count < MIN_PART) count--;
  while (n / count > MAX_PART && count < n) count++;

  const parts: Part[] = [];
  let start = 0;
  for (let k = 0; k < count - 1; k++) {
    const left = count - k; // parts still to place, this one included
    const remaining = n - start;
    const ideal = remaining / left;
    // Leave at least one word for every later part, and never more than MAX.
    const minNow = Math.max(1, remaining - (left - 1) * MAX_PART);
    let lower = Math.max(MIN_PART, minNow);
    let upper = Math.min(MAX_PART, remaining - (left - 1));
    if (lower > upper) lower = upper;
    let size = Math.round(ideal + (random() * 2 - 1) * 2);
    size = Math.max(lower, Math.min(upper, size));

    // When possible, leave MIN_PART for the parts that follow.
    const keep = (left - 1) * MIN_PART;
    if (remaining - size < keep) size = Math.max(lower, remaining - keep);

    // Prefer ending after a comma, a sentence or a paragraph if it is close.
    let best = size;
    let bestScore = boundary(words, breaks, start + size);
    for (const delta of [-2, -1, 1, 2]) {
      const candidate = Math.max(lower, Math.min(upper, size + delta));
      if (remaining - candidate < keep) continue;
      const score = boundary(words, breaks, start + candidate);
      if (score > bestScore || (score === bestScore && Math.abs(candidate - ideal) < Math.abs(best - ideal))) {
        best = candidate;
        bestScore = score;
      }
    }
    parts.push({ start, end: start + best });
    start += best;
  }

  if (start < n) parts.push({ start, end: n });

  // Repeat the tail of each range at the start of the next one.
  for (let i = 1; i < parts.length; i++) {
    parts[i] = { start: Math.max(0, parts[i].start - OVERLAP), end: parts[i].end };
  }
  return parts;
}

/** PointedWord is the part of a measured word that matters for hit testing. */
export interface PointedWord {
  rects: readonly Rect[];
}

/**
 * partAtPoint maps a point inside the prompt to the part containing the
 * nearest word. A tap on the padding between words or lines still lands on a
 * part instead of doing nothing.
 */
export function partAtPoint(words: readonly PointedWord[], parts: readonly Part[], x: number, y: number): number {
  let best = -1;
  let bestDistance = Infinity;
  for (let i = 0; i < words.length; i++) {
    for (const r of words[i].rects) {
      const dx = x < r.left ? r.left - x : x > r.left + r.width ? x - (r.left + r.width) : 0;
      const dy = y < r.top ? r.top - y : y > r.top + r.height ? y - (r.top + r.height) : 0;
      const distance = dx * dx + dy * dy;
      if (distance < bestDistance) {
        best = i;
        bestDistance = distance;
        if (distance === 0) break;
      }
    }
    if (bestDistance === 0) break;
  }
  if (best < 0) return -1;
  // Ranges overlap: choose the latest one, so tapping the repeated context
  // moves forward instead of back.
  for (let i = parts.length - 1; i >= 0; i--) {
    if (best >= parts[i].start && best < parts[i].end) return i;
  }
  return -1;
}

/**
 * pauseFor is the short gap between ranges. It grows only at a real sentence
 * or paragraph boundary, so the guide keeps flowing instead of stopping at
 * every arbitrary chunk edge.
 */
export function pauseFor(words: readonly string[], breaks: readonly boolean[], part: Part, speed: Timings): number {
  let ms = speed.pauseMs;
  const last = words[part.end - 1] ?? "";
  if (SENTENCE.test(last)) ms += speed.sentenceMs;
  else if (CLAUSE.test(last)) ms += Math.round(speed.sentenceMs * 0.5);
  if (breaks[part.end]) ms += speed.paragraphMs;
  return ms;
}

const num = (v: number) => String(Math.round(v * 100) / 100);

/**
 * rectsToPath turns the visible word boxes into one disjoint clip path.
 * Padding keeps descenders and inline padding inside the revealed range.
 */
export function rectsToPath(rects: readonly Rect[], pad = 2, bounds?: { width: number; height: number }): string {
  const out: string[] = [];
  for (const r of rects) {
    const x1 = Math.max(0, r.left - pad);
    const y1 = Math.max(0, r.top - pad);
    let x2 = r.left + r.width + pad;
    let y2 = r.top + r.height + pad;
    if (bounds) {
      x2 = Math.min(bounds.width, x2);
      y2 = Math.min(bounds.height, y2);
    }
    if (x2 <= x1 || y2 <= y1) continue;
    out.push(`M${num(x1)} ${num(y1)}H${num(x2)}V${num(y2)}H${num(x1)}Z`);
  }
  return out.join("");
}
