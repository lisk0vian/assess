import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { ChevronLeft, ChevronRight, Pause, Play, RotateCcw } from "lucide-react";
import { t } from "../i18n";
import { iconProps } from "./ui";
import { OVERLAP, partAtPoint, pauseFor, rectsToPath, splitParts, timingsOf, type Part, type Rect } from "./reading";

// The lens, against photos of the screen (a phone camera plus a vision AI):
// the prompt is read in ranges of words, one range at a time. The first time
// a question opens, the ranges are read automatically; after that the student
// taps a zone to jump there, or moves with the controls, swipe or keyboard.
// The text is masked, not removed, so screen readers still read the whole
// prompt.

interface Word {
  text: string;
  rects: Rect[];
  breakBefore: boolean;
}

type Line = Rect;

interface Measured {
  words: Word[];
  lines: Line[];
  width: number;
  height: number;
  signature: string;
}

const ATOMIC = "img, svg, .katex, .katex-display, .math-block";
const SKIP = "script, style, .ln, .code__head, .sr-only";
const BLOCKS = new Set([
  "ADDRESS", "ARTICLE", "ASIDE", "BLOCKQUOTE", "DIV", "DL", "FIGCAPTION", "FIGURE", "FOOTER",
  "H1", "H2", "H3", "H4", "H5", "H6", "HEADER", "HR", "LI", "MAIN", "NAV", "OL", "P", "PRE",
  "SECTION", "TABLE", "TBODY", "TD", "TFOOT", "TH", "THEAD", "TR", "UL",
]);
const AUTO_DELAY = 500;
// The text appears quickly; the reading guide moves at the teacher's pace.
const TYPE_MS = 35;

function relative(rects: ArrayLike<DOMRect>, box: DOMRect): Rect[] {
  return Array.from(rects)
    .map((r) => ({
      left: Math.max(0, r.left - box.left),
      top: Math.max(0, r.top - box.top),
      width: Math.min(box.width, r.right - box.left) - Math.max(0, r.left - box.left),
      height: Math.min(box.height, r.bottom - box.top) - Math.max(0, r.top - box.top),
    }))
    .filter((r) => r.width > 0.5 && r.height > 0.5);
}

/** measurePrompt maps the rendered words and lines to boxes inside el. */
function measurePrompt(el: HTMLElement): Measured {
  const box = el.getBoundingClientRect();
  const words: Word[] = [];
  let breakNext = false;

  const add = (text: string, rects: Rect[]) => {
    if (!rects.length) return;
    words.push({ text, rects, breakBefore: breakNext || words.length === 0 });
    breakNext = false;
  };

  const walk = (node: Node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      const text = node.textContent ?? "";
      const re = /\S+/g;
      let m: RegExpExecArray | null;
      while ((m = re.exec(text))) {
        const range = document.createRange();
        range.setStart(node, m.index);
        range.setEnd(node, m.index + m[0].length);
        add(m[0], relative(range.getClientRects(), box));
      }
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const child = node as HTMLElement;
    if (child.matches(SKIP)) return;
    if (child.matches(ATOMIC)) {
      add((child.textContent ?? "").trim() || child.getAttribute("alt") || "imagen", relative(child.getClientRects(), box));
      return;
    }
    if (child.tagName === "BR") {
      breakNext = true;
      return;
    }
    const block = BLOCKS.has(child.tagName);
    if (block) breakNext = true;
    child.childNodes.forEach(walk);
    if (block) breakNext = true;
  };

  walk(el);

  // One veil per rendered line; merge word boxes that share a line.
  const lines: Line[] = [];
  const raw = words.flatMap((w) => w.rects).sort((a, b) => a.top - b.top || a.left - b.left);
  for (const r of raw) {
    const last = lines[lines.length - 1];
    const overlap = last ? Math.min(last.top + last.height, r.top + r.height) - Math.max(last.top, r.top) : 0;
    if (last && overlap > Math.min(last.height, r.height) / 2) {
      const right = Math.max(last.left + last.width, r.left + r.width);
      const bottom = Math.max(last.top + last.height, r.top + r.height);
      last.left = Math.min(last.left, r.left);
      last.top = Math.min(last.top, r.top);
      last.width = right - last.left;
      last.height = bottom - last.top;
    } else {
      lines.push({ ...r });
    }
  }

  return {
    words,
    lines,
    width: box.width,
    height: box.height,
    signature: words.map((w) => w.text).join("\u0001"),
  };
}

/** subtract takes the revealed word boxes out of a line's veil. */
function veilSegments(line: Line, holes: readonly Rect[]) {
  let spans = [{ left: line.left, right: line.left + line.width }];
  for (const h of holes) {
    const overlap = Math.min(line.top + line.height, h.top + h.height) - Math.max(line.top, h.top);
    if (overlap <= 0) continue;
    const left = Math.max(line.left, h.left - 2);
    const right = Math.min(line.left + line.width, h.left + h.width + 2);
    if (right <= left) continue;
    spans = spans.flatMap((s) => {
      if (right <= s.left || left >= s.right) return [s];
      const out: { left: number; right: number }[] = [];
      if (left > s.left) out.push({ left: s.left, right: left });
      if (right < s.right) out.push({ left: right, right: s.right });
      return out;
    });
  }
  return spans.filter((s) => s.right - s.left > 1).map((s) => ({ left: s.left, width: s.right - s.left }));
}

function useReducedMotion() {
  const [reduced, setReduced] = useState(
    () => typeof matchMedia !== "undefined" && matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    const mq = matchMedia("(prefers-reduced-motion: reduce)");
    const change = () => setReduced(mq.matches);
    mq.addEventListener("change", change);
    return () => mq.removeEventListener("change", change);
  }, []);
  return reduced;
}

interface LensProps {
  on: boolean;
  /** The teacher's auto-read switch, and whether this is the first view. */
  autoPlay: boolean;
  /** Auto-read is available for this question (the teacher did not turn it off). */
  autoEnabled?: boolean;
  /** The hint for hover-revealed options, shown on the controls line. */
  answerHint?: string;
  readSpeed: string;
  /** Pause auto-read (teacher pause or the student is answering). */
  hold?: boolean;
  /** Last part read on a previous visit to this question. */
  initialPart?: number;
  onPartChange?: (index: number) => void;
  describedBy?: string;
  onAutoPlayed?: () => void;
  children: ReactNode;
}

export function Lens({ on, autoPlay, autoEnabled = false, answerHint, readSpeed, hold = false, initialPart, onPartChange, describedBy, onAutoPlayed, children }: LensProps) {
  const root = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const [measured, setMeasured] = useState<Measured | null>(null);
  const [active, setActive] = useState<number | null>(null);
  const [revealed, setRevealed] = useState(0);
  const [guide, setGuide] = useState(0);
  const [phase, setPhase] = useState<"idle" | "typing" | "shown">("idle");
  const [auto, setAuto] = useState(false);
  const [autoPaused, setAutoPaused] = useState(false);
  const reduced = useReducedMotion();
  // Re-measuring on resize must not restart the auto-read timer.
  const measuredRef = useRef<Measured | null>(null);
  measuredRef.current = measured;

  const signature = measured?.signature ?? "";
  const parts = useMemo<Part[]>(
    () => (measured ? splitParts(measured.words.map((w) => w.text), measured.words.map((w) => w.breakBefore)) : []),
    // A resize changes the boxes, not the ranges: only a text change reshuffles.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [signature],
  );
  const timings = timingsOf(readSpeed);
  const autoTimer = useRef<number | undefined>(undefined);
  const autoStarted = useRef(false);
  const tookOver = useRef(false);
  const lastSignature = useRef("");
  const down = useRef<{ x: number; y: number } | null>(null);
  const skipScroll = useRef(false);
  const onAuto = useRef(onAutoPlayed);
  onAuto.current = onAutoPlayed;
  const onPart = useRef(onPartChange);
  onPart.current = onPartChange;

  // Measure after every layout change. Fullscreen changes the width, and the
  // old marks must never remain over the new lines.
  useEffect(() => {
    if (!on) return;
    const el = body.current;
    if (!el) return;
    const measure = () => setMeasured(measurePrompt(el));
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    const mo = new MutationObserver(measure);
    mo.observe(el, { childList: true, subtree: true, characterData: true });
    const afterLayout = () => requestAnimationFrame(() => requestAnimationFrame(measure));
    window.addEventListener("resize", afterLayout);
    document.addEventListener("fullscreenchange", afterLayout);
    void document.fonts?.ready.then(measure);
    return () => {
      ro.disconnect();
      mo.disconnect();
      window.removeEventListener("resize", afterLayout);
      document.removeEventListener("fullscreenchange", afterLayout);
    };
  }, [on]);

  const startPart = useCallback(
    (index: number, autoMode: boolean, instant = false) => {
      const part = parts[index];
      if (!part) return;
      setActive(index);
      setAuto(autoMode);
      setAutoPaused(false);
      // Keep the repeated tail visible while the new words are typed.
      const total = part.end - part.start;
      const warm = index > 0 ? Math.min(OVERLAP, total) : 0;
      setRevealed(instant || reduced ? total : warm);
      setGuide(instant || reduced ? total : warm);
      setPhase(instant || reduced ? "shown" : "typing");
      onPart.current?.(index);
    },
    [parts, reduced],
  );

  const complete = useCallback(() => {
    if (active === null) return;
    const part = parts[active];
    if (!part) return;
    setRevealed(part.end - part.start);
    setGuide(part.end - part.start);
    setPhase("shown");
  }, [active, parts]);

  const hide = useCallback(() => {
    setActive(null);
    setRevealed(0);
    setGuide(0);
    setPhase("idle");
    setAuto(false);
    setAutoPaused(false);
  }, []);

  useEffect(() => {
    if (!on) hide();
  }, [hide, on]);

  // A different prompt starts over. With auto-read off, the first range is
  // visible at once; with auto-read on, the pass below fills it.
  useEffect(() => {
    if (lastSignature.current !== signature) {
      lastSignature.current = signature;
      autoStarted.current = false;
      tookOver.current = false;
    }
    setAuto(false);
    setAutoPaused(false);
    if (!on || !parts.length) {
      setActive(null);
      setRevealed(0);
      setGuide(0);
      setPhase("idle");
      return;
    }
    if (!autoPlay || reduced) {
      const first = initialPart != null && initialPart >= 0 && initialPart < parts.length ? initialPart : 0;
      startPart(first, false, true);
    } else {
      setActive(null);
      setRevealed(0);
      setGuide(0);
      setPhase("idle");
    }
    // initialPart is a mount value; reacting to its updates would rewind the
    // lens every time the parent records the current part.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature, on]);

  const takeOver = useCallback(() => {
    window.clearTimeout(autoTimer.current);
    tookOver.current = true;
    if (!autoStarted.current) onAuto.current?.();
    setAuto(false);
    setAutoPaused(false);
    if (phase === "typing") complete();
  }, [complete, phase]);

  const next = useCallback(() => {
    takeOver();
    if (active === null) startPart(0, false, true);
    else if (active < parts.length - 1) startPart(active + 1, false, true);
  }, [active, parts.length, startPart, takeOver]);

  const prev = useCallback(() => {
    takeOver();
    if (active === null) startPart(0, false, true);
    else if (active > 0) startPart(active - 1, false, true);
  }, [active, startPart, takeOver]);

  const jumpTo = useCallback(
    (index: number, fromTap = false) => {
      if (index < 0 || index >= parts.length) return;
      takeOver();
      // The tapped range is already under the finger; don't yank the page.
      skipScroll.current = fromTap && index !== active;
      startPart(index, false, true);
    },
    [active, parts.length, startPart, takeOver],
  );

  const togglePause = useCallback(() => {
    if (!auto) return;
    setAutoPaused((p) => !p);
  }, [auto]);

  const replay = useCallback(() => {
    window.clearTimeout(autoTimer.current);
    autoStarted.current = true;
    tookOver.current = false;
    startPart(0, true);
  }, [startPart]);

  // The first view reads the prompt once, after the layout is stable.
  useEffect(() => {
    if (!on || !measured || !autoPlay || reduced || hold || autoStarted.current || tookOver.current || !parts.length) return;
    autoTimer.current = window.setTimeout(() => {
      autoStarted.current = true;
      onAuto.current?.();
      startPart(0, true);
    }, AUTO_DELAY);
    return () => window.clearTimeout(autoTimer.current);
  }, [autoPlay, hold, measured, on, parts.length, reduced, startPart]);

  // Pausing the student, or focusing an answer, holds the automatic pass.
  // It never resumes by itself; the button says Reanudar, and the student can
  // still choose to resume even if the answer keeps focus.
  useEffect(() => {
    if (hold && auto) setAutoPaused(true);
  }, [auto, hold]);

  // The text appears quickly; it can be read ahead of the guide.
  useEffect(() => {
    if (phase !== "typing" || active === null || autoPaused) return;
    const part = parts[active];
    if (!part) return;
    if (revealed >= part.end - part.start) {
      setPhase("shown");
      return;
    }
    const timer = window.setTimeout(() => setRevealed((n) => n + 1), TYPE_MS);
    return () => window.clearTimeout(timer);
  }, [active, autoPaused, parts, phase, revealed]);

  // The guide follows the teacher's reading pace without waiting for the
  // text pass: the next word is always revealed long before the guide reaches
  // it, so a range change stays fluid.
  useEffect(() => {
    if (!auto || autoPaused || active === null) return;
    const part = parts[active];
    if (!part) return;
    const total = part.end - part.start;
    if (guide >= total) return;
    const timer = window.setTimeout(() => setGuide((g) => Math.min(total, g + 1)), timings.wordMs);
    return () => window.clearTimeout(timer);
  }, [active, auto, autoPaused, guide, parts, timings.wordMs]);

  // Auto-read: after the guide reaches the end, leave the range for a moment,
  // then move on. The last range closes the prompt again.
  useEffect(() => {
    const measure = measuredRef.current;
    if (!auto || autoPaused || active === null || !measure) return;
    const part = parts[active];
    if (!part) return;
    const total = part.end - part.start;
    if (guide < total || revealed < total) return;
    const last = active >= parts.length - 1;
    const timer = window.setTimeout(() => {
      if (last) {
        hide();
        return;
      }
      startPart(active + 1, true);
    }, pauseFor(measure.words.map((w) => w.text), measure.words.map((w) => w.breakBefore), part, timings));
    return () => window.clearTimeout(timer);
  }, [active, auto, autoPaused, guide, hide, parts, revealed, startPart, timings]);

  // Keep the range being read on screen, not below the mobile bar or above
  // the sticky header.
  useEffect(() => {
    if (!on || active === null || !measured) return;
    if (skipScroll.current) {
      skipScroll.current = false;
      return;
    }
    const part = parts[active];
    const rect = part ? measured.words[part.start]?.rects[0] : undefined;
    if (!rect) return;
    const raf = requestAnimationFrame(() => {
      const el = body.current;
      if (!el) return;
      const box = el.getBoundingClientRect();
      const top = box.top + rect.top;
      const bottom = top + rect.height;
      const narrow = window.innerWidth <= 960;
      const insetTop = narrow ? 132 : 108;
      const insetBottom = narrow ? 112 : 24;
      if (top < insetTop || bottom > window.innerHeight - insetBottom) {
        window.scrollBy({ top: top - window.innerHeight / 2, behavior: reduced ? "auto" : "smooth" });
      }
    });
    return () => cancelAnimationFrame(raf);
  }, [active, measured, on, parts, reduced]);

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    down.current = { x: e.clientX, y: e.clientY };
  };

  const onPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    const start = down.current;
    down.current = null;
    if (!start || !body.current || !measured) return;
    const dx = e.clientX - start.x;
    const dy = e.clientY - start.y;
    // A deliberate horizontal swipe moves one range; a vertical scroll stays
    // a scroll and a tap jumps to the range under the finger.
    if (e.pointerType === "touch" && Math.abs(dx) > 40 && Math.abs(dx) > Math.abs(dy) * 1.5) {
      if (dx < 0) next();
      else prev();
      return;
    }
    if (Math.hypot(dx, dy) > 8) return;
    const box = body.current.getBoundingClientRect();
    const index = partAtPoint(measured.words, parts, e.clientX - box.left, e.clientY - box.top);
    if (index >= 0) jumpTo(index, true);
  };

  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      e.stopPropagation();
      if (e.shiftKey) prev();
      else next();
    } else if (e.key === "ArrowLeft" || e.key === "Backspace") {
      e.preventDefault();
      e.stopPropagation();
      prev();
    } else if (e.key === "ArrowRight" || e.key === "ArrowDown") {
      e.preventDefault();
      e.stopPropagation();
      next();
    }
  };

  if (!on) return <>{children}</>;

  const activePart = active !== null ? parts[active] : undefined;
  const count = activePart ? Math.min(revealed, activePart.end - activePart.start) : 0;
  const holes = activePart && measured ? measured.words.slice(activePart.start, activePart.start + count).flatMap((w) => w.rects) : [];
  const path = measured ? rectsToPath(holes, 2, measured) : "";
  const clip = path ? `path("${path}")` : "inset(100%)";
  const guideWord = activePart && measured ? measured.words[activePart.start + Math.max(0, guide - 1)] : undefined;
  const guideRect = guideWord?.rects.at(-1);
  const guideDone = activePart ? guide >= activePart.end - activePart.start : false;
  const last = active !== null && active >= parts.length - 1;

  return (
    <div className="lens-frame">
      <div
        ref={root}
        role="group"
        aria-describedby={describedBy}
        aria-keyshortcuts="Enter Space ArrowLeft ArrowRight"
        className={`lens ${active === null ? "is-closed" : ""}`}
        tabIndex={0}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerCancel={() => (down.current = null)}
        onKeyDown={onKey}
      >
        <div ref={body} className="lens__body" style={{ clipPath: clip }}>
          {children}
        </div>
        <div className="lens__veils" aria-hidden="true">
          {measured?.lines.map((line, i) => {
            const h = Math.min(14, line.height * 0.6);
            return veilSegments(line, holes).map((s, j) => (
              <span
                key={`${i}-${j}`}
                className="veil"
                style={{ left: s.left, top: line.top + (line.height - h) / 2, width: s.width, height: h }}
              />
            ));
          })}
        </div>
        {guideRect && measured && (
          <>
            {count > 0 && (
              <span
                aria-hidden="true"
                className="reading-wash"
                style={{
                  left: Math.max(0, guideRect.left - 4),
                  top: guideRect.top,
                  width: Math.min(measured.width, guideRect.left + guideRect.width + 4) - Math.max(0, guideRect.left - 4),
                  height: guideRect.height,
                }}
              />
            )}
            <span
              aria-hidden="true"
              className={`reading-guide ${auto && guideDone ? "is-waiting" : ""}`}
              style={{
                left: Math.max(0, guideRect.left),
                top: guideRect.top + guideRect.height - 3,
                width: Math.max(2, Math.min(measured.width, guideRect.left + guideRect.width) - Math.max(0, guideRect.left)),
              }}
            />
          </>
        )}
      </div>
      {(answerHint || parts.length > 1) && (
        <div className="lens-answer-head">
          {answerHint && <p className="label reveal-hint">{answerHint}</p>}
          {parts.length > 1 && (
            <div className="lens-toolbar" role="group" aria-label={t("lens_toolbar")}>
              <button
                type="button"
                className="btn btn--sm btn--icon btn--quiet"
                onClick={prev}
                disabled={active === null || active === 0}
                aria-label={t("lens_prev")}
              >
                <ChevronLeft {...iconProps} />
              </button>
              <button
                type="button"
                className="btn btn--sm btn--icon btn--quiet"
                onClick={next}
                disabled={last}
                aria-label={t("lens_next")}
              >
                <ChevronRight {...iconProps} />
              </button>
              {autoEnabled &&
                (auto ? (
                  <button
                    type="button"
                    className="btn btn--sm btn--icon btn--quiet"
                    onClick={togglePause}
                    aria-pressed={autoPaused}
                    aria-label={autoPaused ? t("lens_resume") : t("lens_pause")}
                  >
                    {autoPaused ? <Play {...iconProps} /> : <Pause {...iconProps} />}
                  </button>
                ) : (
                  <button
                    type="button"
                    className="btn btn--sm btn--icon btn--quiet"
                    onClick={replay}
                    aria-label={t("lens_restart")}
                  >
                    <RotateCcw {...iconProps} />
                  </button>
                ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
