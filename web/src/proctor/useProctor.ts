import { useEffect, useRef, useState } from "react";
import { telemetry } from "./telemetry";

export type AwayKind = "hidden" | "fullscreen_exit" | "blur";

/** One continuous absence: it counts once, as its most serious kind. */
export interface Away {
  kind: AwayKind; // the most serious state of the episode: what it counts as
  now: AwayKind; // the state right now: what the student must fix to come back
  since: number; // performance.now() when the episode began
}

const rank: Record<AwayKind, number> = { blur: 1, fullscreen_exit: 2, hidden: 3 };

export interface ProctorOptions {
  enabled: boolean;
  fullscreenRequired: boolean;
  /** The question on screen, attached to signals. */
  questionID: string;
  /** Poisoned clipboard text if a copy slips through. */
  clipboardNote: () => string;
}

export const fullscreenSupported = () => typeof document !== "undefined" && !!document.fullscreenEnabled;

/** Returns why the exam is not in view right now, if it is not. */
function currentAway(fullscreenRequired: boolean): AwayKind | null {
  if (document.visibilityState === "hidden") return "hidden";
  if (fullscreenRequired && fullscreenSupported() && !document.fullscreenElement) return "fullscreen_exit";
  if (!document.hasFocus()) return "blur";
  return null;
}

const inField = (t: EventTarget | null) => t instanceof Element && !!t.closest("input, textarea, [contenteditable]");

/**
 * useProctor collects the browser's integrity signals while an exam is on
 * screen and reports them through telemetry. It blocks copying and pasting
 * on the exam, and returns the current absence so the page can draw the
 * privacy curtain and its countdown.
 */
export function useProctor(o: ProctorOptions): Away | null {
  const [away, setAway] = useState<Away | null>(null);
  const opts = useRef(o);
  opts.current = o;

  // Absences: hidden tab, focus elsewhere, out of required fullscreen.
  useEffect(() => {
    if (!o.enabled) return;
    let episode: Away | null = null;
    const q = () => opts.current.questionID;
    const evaluate = () => {
      const kind = currentAway(opts.current.fullscreenRequired);
      // The absence lasts until the exam is fully back. Coming back to the tab
      // but out of fullscreen (switching tabs leaves it) is the same absence:
      // it counts once, and ends when the student restores fullscreen.
      if (!kind) {
        if (episode) {
          telemetry.report(episode.kind, { phase: "end", durationMs: performance.now() - episode.since, questionID: q() });
          episode = null;
          setAway(null);
        }
        return;
      }
      if (!episode) {
        episode = { kind, now: kind, since: performance.now() };
        telemetry.report(kind, { phase: "start", questionID: q() });
        setAway(episode);
      } else if (rank[kind] > rank[episode.kind]) {
        // Same absence, more serious now: the server measures the new kind from here.
        episode = { kind, now: kind, since: episode.since };
        telemetry.report(kind, { phase: "start", questionID: q() });
        setAway(episode);
      } else if (kind !== episode.now) {
        episode = { ...episode, now: kind };
        setAway(episode);
      }
    };
    telemetry.whenAttached(() => {
      if (episode) telemetry.report(episode.kind, { phase: "start", questionID: q() });
    });
    const events = ["visibilitychange", "fullscreenchange"] as const;
    events.forEach((e) => document.addEventListener(e, evaluate));
    window.addEventListener("blur", evaluate);
    window.addEventListener("focus", evaluate);
    const poll = window.setInterval(evaluate, 1000); // focus can move without events
    evaluate();
    return () => {
      telemetry.whenAttached(null);
      events.forEach((e) => document.removeEventListener(e, evaluate));
      window.removeEventListener("blur", evaluate);
      window.removeEventListener("focus", evaluate);
      window.clearInterval(poll);
    };
  }, [o.enabled]);

  // Copy, paste, typing forensics, automation, injected DOM, split screen.
  useEffect(() => {
    if (!o.enabled) return;
    const q = () => opts.current.questionID;
    const throttled = new Map<string, number>();
    const once = (kind: string, everyMs: number) => {
      const now = performance.now();
      const last = throttled.get(kind);
      if (last !== undefined && now - last < everyMs) return;
      throttled.set(kind, now);
      telemetry.report(kind, { questionID: q() });
    };

    const onCopy = (e: ClipboardEvent) => {
      if (inField(e.target)) return; // the student's own answer text
      e.preventDefault();
      e.clipboardData?.setData("text/plain", opts.current.clipboardNote());
      once("copy", 1500);
    };
    const onPaste = (e: ClipboardEvent) => {
      e.preventDefault();
      if (inField(e.target)) once("paste_input", 1500);
    };
    const onBeforeInput = (e: InputEvent) => {
      if (/^insertFrom(Paste|Drop|Yank|PasteAsQuotation)$/.test(e.inputType)) {
        e.preventDefault();
        once("paste_input", 1500);
      }
    };
    // Typing bursts: more text in a second than a person types.
    let windowStart = 0;
    let typed = 0;
    const burstFor = new Set<string>();
    const onInput = (e: Event) => {
      const ie = e as InputEvent;
      if (ie.inputType !== "insertText" || !ie.data) return;
      const now = performance.now();
      if (now - windowStart > 1000) {
        windowStart = now;
        typed = 0;
      }
      typed += ie.data.length;
      if (typed > 40 && !burstFor.has(q())) {
        burstFor.add(q());
        telemetry.report("burst", { questionID: q() });
      }
    };
    const block = (e: Event) => {
      if (!inField(e.target)) e.preventDefault();
    };
    // Screen captures. Browsers cannot block the OS, so: hide the exam while
    // the Windows/Command key is held (Win+Shift+S, Cmd+Shift+4 start with
    // it), replace a PrintScreen image in the clipboard with a notice, and
    // never print.
    const root = document.documentElement;
    const shield = (on: boolean) => root.classList.toggle("is-shielded", on);
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Meta" || e.key === "OS") shield(true);
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "p") {
        e.preventDefault();
        once("screenshot", 1500);
      }
    };
    const onKeyUp = (e: KeyboardEvent) => {
      if (e.key === "Meta" || e.key === "OS") shield(false);
      if (e.key === "PrintScreen") {
        poisonClipboard(opts.current.clipboardNote());
        once("screenshot", 1500);
      }
    };
    const unshield = () => shield(false);
    const onBeforePrint = () => once("screenshot", 1500);
    const onUntrusted = (e: Event) => {
      if (!e.isTrusted) once("untrusted", 10_000);
    };
    const onDrop = (e: DragEvent) => {
      e.preventDefault();
      if (inField(e.target)) once("paste_input", 1500);
    };

    document.addEventListener("copy", onCopy);
    document.addEventListener("cut", onCopy);
    document.addEventListener("paste", onPaste);
    document.addEventListener("beforeinput", onBeforeInput);
    document.addEventListener("input", onInput);
    document.addEventListener("contextmenu", block);
    document.addEventListener("selectstart", block);
    document.addEventListener("dragstart", block);
    document.addEventListener("drop", onDrop);
    document.addEventListener("keyup", onKeyUp);
    document.addEventListener("keydown", onKeyDown, true);
    window.addEventListener("focus", unshield);
    window.addEventListener("beforeprint", onBeforePrint);
    for (const t of ["click", "keydown", "input"]) document.addEventListener(t, onUntrusted, true);

    if (navigator.webdriver) once("webdriver", Infinity);

    // Extensions that write into the page. Ours: #root, #portal and our own
    // lazily loaded assets.
    const foreign = (n: Node) => {
      if (!(n instanceof Element)) return false;
      if (n.id === "root" || n.id === "portal") return false;
      const src = n.getAttribute("src") ?? n.getAttribute("href") ?? "";
      if ((n.tagName === "SCRIPT" || n.tagName === "LINK" || n.tagName === "STYLE") && (src.startsWith("/") || src.startsWith(location.origin) || n.hasAttribute("data-vite-dev-id"))) return false;
      return true;
    };
    const scan = (nodes: Iterable<Node>) => {
      for (const n of nodes) if (foreign(n)) return once("dom_injection", Infinity);
    };
    scan([...document.body.children].filter((n) => n.tagName !== "SCRIPT"));
    const mo = new MutationObserver((records) => records.forEach((r) => scan(r.addedNodes)));
    mo.observe(document.body, { childList: true });
    mo.observe(document.head, { childList: true });

    // Split screen: the window shrinks well below the screen.
    let wide = window.innerWidth >= screen.availWidth * 0.85;
    const onResize = () => {
      const w = window.innerWidth;
      if (wide && w < screen.availWidth * 0.6) once("resize", 30_000);
      wide = w >= screen.availWidth * 0.85;
    };
    window.addEventListener("resize", onResize);

    return () => {
      document.removeEventListener("copy", onCopy);
      document.removeEventListener("cut", onCopy);
      document.removeEventListener("paste", onPaste);
      document.removeEventListener("beforeinput", onBeforeInput);
      document.removeEventListener("input", onInput);
      document.removeEventListener("contextmenu", block);
      document.removeEventListener("selectstart", block);
      document.removeEventListener("dragstart", block);
      document.removeEventListener("drop", onDrop);
      document.removeEventListener("keyup", onKeyUp);
      document.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("focus", unshield);
      window.removeEventListener("beforeprint", onBeforePrint);
      shield(false);
      for (const t of ["click", "keydown", "input"]) document.removeEventListener(t, onUntrusted, true);
      mo.disconnect();
      window.removeEventListener("resize", onResize);
    };
  }, [o.enabled]);

  return away;
}

/**
 * poisonClipboard replaces the clipboard (a PrintScreen image) with a notice.
 * navigator.clipboard needs a secure context; execCommand("copy") still works
 * on plain HTTP inside a key event. The textarea goes in #portal, which the
 * injected-DOM watcher treats as ours.
 */
function poisonClipboard(text: string) {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
  (document.getElementById("portal") ?? document.body).appendChild(ta);
  ta.select();
  try {
    document.execCommand("copy");
  } catch {
    /* nothing else to try over HTTP */
  }
  ta.remove();
}

/** Desktop-only exams turn away phones and tablets (no fine pointer at all). */
export function isTouchOnly(): boolean {
  return matchMedia("(pointer: coarse)").matches && !matchMedia("(any-pointer: fine)").matches;
}
