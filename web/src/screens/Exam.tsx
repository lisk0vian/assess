import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { ArrowLeft, ArrowRight, List, Timer, X } from "lucide-react";
import { api, ApiError, type Response } from "../api/client";
import type { MeView, QuestionView } from "../api/types";
import { points, t, type Key } from "../i18n";
import { AnswerQueue } from "../state/answerQueue";
import { Answer } from "../components/Answer";
import { Markdown } from "../components/Markdown";
import { Lens } from "../components/Lens";
import { clock, iconProps, RecordRow, SaveStatus, timerStage, Toasts, Wordmark, type Toast } from "../components/ui";
import { fullscreenSupported, isTouchOnly, useProctor } from "../proctor/useProctor";
import { Curtain, hhmm, Strikes, Watermark } from "../proctor/ProctorUI";
import { uid } from "../uid";

interface Props {
  me: MeView;
  now: () => number;
  setMe: (me: MeView) => void;
  reportAt: (questionID: string) => void;
  /** The teacher paused this student (or everyone). */
  paused: boolean;
}

const typeKey: Record<string, Key> = { single: "t_single", multiple: "t_multiple", truefalse: "t_truefalse", short: "t_short" };
const pad = (n: number) => String(n).padStart(2, "0");
const portal = () => document.getElementById("portal") ?? undefined;
const answered = (r: Response | undefined) => !!r && (!!r.option || !!r.options?.length || r.value !== undefined || !!r.text?.trim());

export function Exam({ me, now, setMe, reportAt, paused }: Props) {
  const questions = me.questions;
  // Where the student was, so a reload or a readmission resumes there.
  const posKey = `assess-pos:${me.exam.id}:${me.student.student_id}`;
  const [index, setIndex] = useState(() => {
    try {
      const i = Number(localStorage.getItem(posKey));
      return Number.isInteger(i) && i >= 0 && i < questions.length ? i : 0;
    } catch {
      return 0;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(posKey, String(index));
    } catch {
      /* storage may be unavailable */
    }
  }, [index, posKey]);
  const [view, setView] = useState<"question" | "review">("question");
  const [answers, setAnswers] = useState<Record<string, Response>>(me.answers ?? {});
  const [cache, setCache] = useState<Record<string, QuestionView>>({});
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [indexOpen, setIndexOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  // Timed questions the student chose to open in this page (opening starts their clock).
  const [opened, setOpened] = useState<Set<string>>(() => new Set(questions.filter((x) => x.closes_at).map((x) => x.id)));
  // The student is focusing an answer: hold the automatic reading pass.
  const [answering, setAnswering] = useState(false);

  // Questions already read automatically in this page; the teacher can turn
  // the automatic first pass off, but it never repeats on a revisit.
  const autoSeen = useRef<Set<string>>(new Set());
  // Where the student left the lens on each question (this page only).
  const seenParts = useRef<Map<string, number>>(new Map());

  useEffect(() => {
    setAnswering(false);
  }, [index, view]);

  // Flags are a study aid, kept in this browser only.
  const flagKey = `assess-flags:${me.exam.id}:${me.student.student_id}`;
  const [flags, setFlags] = useState<Set<string>>(() => {
    try {
      return new Set(JSON.parse(localStorage.getItem(flagKey) ?? "[]") as string[]);
    } catch {
      return new Set();
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(flagKey, JSON.stringify([...flags]));
    } catch {
      /* storage may be unavailable */
    }
  }, [flags, flagKey]);

  // Answer queue: optimistic UI, ordered retries, survives network drops.
  const queue = useRef<AnswerQueue | null>(null);
  if (queue.current === null) queue.current = new AnswerQueue(me.attempt.last_seq);
  const q = queue.current;
  useEffect(() => q.sync(me.attempt.last_seq), [q, me.attempt.last_seq]);
  const saveState = useSyncExternalStore(
    (fn) => q.subscribe(fn),
    () => `${q.state}:${q.waiting}`,
  );

  // Clock: ticks every second against the server-aligned time.
  const [tick, setTick] = useState(now());
  useEffect(() => {
    const id = window.setInterval(() => setTick(now()), 1000);
    return () => window.clearInterval(id);
  }, [now]);
  const remaining = (me.attempt.deadline ?? 0) - tick;
  const stage = timerStage(remaining);
  const timeUp = remaining <= 0;

  const toast = useCallback((n: Omit<Toast, "id">) => setToasts((all) => [...all.filter((x) => x.title !== n.title), { ...n, id: uid() }]), []);

  // Extra time granted: tell the student until they dismiss it.
  const lastDeadline = useRef(me.attempt.deadline ?? 0);
  useEffect(() => {
    const d = me.attempt.deadline ?? 0;
    const gained = d - lastDeadline.current;
    lastDeadline.current = d;
    if (gained >= 60_000) toast({ kind: "yours", title: t("extra_time_title", { n: Math.round(gained / 60_000) }), body: t("extra_time_body") });
  }, [me.attempt.deadline, toast]);

  // One calm five-minute warning.
  const warned = useRef(false);
  useEffect(() => {
    if (!warned.current && remaining > 0 && remaining <= 5 * 60_000) {
      warned.current = true;
      toast({ kind: "plain", title: t("warn_title_5"), body: t("warn_body_5") });
    }
  }, [remaining, toast]);

  // Fetch the current question and prefetch the next one.
  const load = useCallback(
    async (i: number) => {
      const id = questions[i]?.id;
      if (!id || cache[id]) return;
      if (questions[i].time_limit_ms && !opened.has(id)) return;
      try {
        const qv = await api.question(id);
        setCache((c) => ({ ...c, [id]: qv }));
      } catch (err) {
        if (!(err instanceof ApiError) || !err.retryable) return;
        window.setTimeout(() => void load(i), 2000);
      }
    },
    [questions, cache, opened],
  );
  useEffect(() => {
    void load(index);
    void load(index + 1);
  }, [index, load]);

  const current = questions[index];
  const question = current ? cache[current.id] : undefined;
  const answerHint = question?.reveal === "hover" ? t(isTouchOnly() ? "reveal_touch" : "reveal_hint") : undefined;

  // Integrity: signals, curtain, strikes. Only with a telemetry key.
  const proc = me.proctoring;
  const watching = !!proc.key;
  // Stillness counts only while a question is on screen: not in the review,
  // before a timed question is opened, or while the teacher has paused.
  const onSheet = view === "question" && !!question && !paused && !(current?.time_limit_ms && !opened.has(current.id));
  const away = useProctor({
    enabled: watching,
    fullscreenRequired: proc.fullscreen === "required",
    questionID: current?.id ?? "",
    clipboardNote: () => t("clipboard_note", { id: me.student.student_id, time: hhmm() }),
    frozenMs: proc.frozen_pointer && onSheet ? proc.thresholds.frozen_ms : 0,
  });
  useEffect(() => {
    if (watching && proc.fullscreen === "suggested" && fullscreenSupported() && !document.fullscreenElement) {
      toast({
        kind: "plain",
        title: t("fs_suggest_title"),
        body: t("fs_suggest_body"),
        action: { label: t("fs_enter"), run: () => void document.documentElement.requestFullscreen().catch(() => undefined) },
      });
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Per-question state: voided by a penalty, or a time limit that ran out.
  const voided = !!(question?.voided ?? current?.voided);
  const limitMs = current?.time_limit_ms ?? 0;
  const closesAt = question?.closes_at ?? current?.closes_at ?? 0;
  const qLeft = closesAt ? closesAt - tick : 0;
  const qClosed = !!closesAt && qLeft <= 0;
  const gated = !!limitMs && !opened.has(current?.id ?? "");
  const locked = timeUp || voided || qClosed;
  useEffect(() => {
    if (current && view === "question") reportAt(current.id);
  }, [current, view, reportAt]);

  const change = (r: Response) => {
    if (!current || locked) return;
    setAnswers((a) => ({ ...a, [current.id]: r }));
    q.put(current.id, r);
  };
  const go = (i: number) => {
    setView("question");
    setIndex(Math.max(0, Math.min(questions.length - 1, i)));
    window.scrollTo({ top: 0, behavior: "instant" });
  };
  const toggleFlag = () =>
    setFlags((f) => {
      const n = new Set(f);
      if (current && !n.delete(current.id)) n.add(current.id);
      return n;
    });

  // Keyboard: A–J or 1–9 choose, ←/→ move, F flags. Never while typing.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (view !== "question" || e.ctrlKey || e.metaKey || e.altKey) return;
      // The prompt takes Enter, Space and the arrows while it has focus.
      if (e.target instanceof Element && e.target.closest("input, textarea, .lens")) return;
      if (e.key === "ArrowRight") go(index + 1);
      else if (e.key === "ArrowLeft") go(index - 1);
      else if (e.key.toLowerCase() === "f") toggleFlag();
      else if (question?.options && /^[a-j1-9]$/i.test(e.key)) {
        const i = /\d/.test(e.key) ? Number(e.key) - 1 : e.key.toLowerCase().charCodeAt(0) - 97;
        const opt = question.options[i];
        if (!opt) return;
        if (question.type === "multiple") {
          const set = new Set(answers[question.id]?.options ?? []);
          if (!set.delete(opt.id)) set.add(opt.id);
          const ordered = question.options.map((o) => o.id).filter((o) => set.has(o));
          change(ordered.length ? { options: ordered } : {});
        } else change({ option: opt.id });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const submit = async () => {
    setSubmitting(true);
    try {
      q.close();
      setMe(await api.submit());
    } catch (err) {
      setSubmitting(false);
      if (err instanceof ApiError && (err.code === "attempt_closed" || err.code === "time_up")) setMe(await api.me());
    }
  };

  const stateOf = (id: string, i: number): { key: Key; swatch: string } => {
    if (i === index && view === "question") return { key: "st_here", swatch: answered(answers[id]) ? "swatch--done" : "" };
    const sum = questions[i] ?? questions.find((x) => x.id === id);
    if (sum?.voided || cache[id]?.voided) return { key: "st_voided", swatch: "swatch--void" };
    if (flags.has(id)) return { key: "st_flagged", swatch: "swatch--flag" };
    if (answered(answers[id])) return { key: "st_answered", swatch: "swatch--done" };
    return { key: "st_pending", swatch: "" };
  };
  const counts = useMemo(() => {
    const done = questions.filter((x) => answered(answers[x.id])).length;
    return { done, empty: questions.length - done, flagged: questions.filter((x) => flags.has(x.id)).length };
  }, [questions, answers, flags]);

  const [saveKind, waitingStr] = saveState.split(":");
  const rail = (
    <ol className="rail">
      {questions.map((x, i) => {
        const s = stateOf(x.id, i);
        return (
          <li key={x.id} className={`rail__row ${s.key === "st_here" ? "is-current" : ""}`} aria-current={s.key === "st_here" ? "step" : undefined} onClick={() => { go(i); setIndexOpen(false); }}>
            <b className="rail__num">{pad(i + 1)}</b>
            {t(s.key)}
            <i className={`swatch ${s.swatch}`} />
          </li>
        );
      })}
    </ol>
  );

  return (
    <div className={`exam ${watching ? "is-guarded" : ""}`}>
      <header className="cells">
        <div className="cell">
          <Wordmark />
          <span className="label desktop-only">{t("in_progress")}</span>
          {watching ? (
            <span className="mobile-only">
              <Strikes n={proc.strikes} limit={proc.strike_limit} compact />
            </span>
          ) : (
            <span className="label mobile-only" style={{ maxWidth: "52vw", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{me.exam.title}</span>
          )}
        </div>
        <div className="cell desktop-only">
          <span className="label">{t("exam")}</span>
          <b>{me.exam.title}</b>
        </div>
        <div className="cell desktop-only">
          <span className="label">{t("student")}</span>
          <b>{me.student.name} · {me.student.student_id}</b>
        </div>
        {watching && (
          <div className="cell desktop-only">
            <span className="label">{t("warnings")}</span>
            <Strikes n={proc.strikes} limit={proc.strike_limit} />
          </div>
        )}
        <div className={`cell timer ${stage.className}`} style={{ border: 0, minWidth: 0 }} role="timer" aria-live="off">
          <span className="label">{t(stage.label)}</span>
          <span className="clock">{clock(remaining)}</span>
        </div>
      </header>

      <div className="strip" style={{ ["--n" as string]: questions.length }} aria-hidden="true">
        {questions.map((x, i) => {
          const s = stateOf(x.id, i);
          const cls = s.key === "st_flagged" ? "flag" : s.swatch === "swatch--done" ? "done" : "";
          return <i key={x.id} className={`${cls} ${s.key === "st_here" ? "cur" : ""}`} />;
        })}
      </div>

      <aside className="side-rail">
        <span className="label">{t("route", { n: questions.length })}</span>
        {rail}
        <SaveStatus state={saveKind as "saved"} waiting={Number(waitingStr)} />
      </aside>

      <main className="desk">
        {view === "question" && current ? (
          <div className="stack">
            <div className="stack__under stack__under--far" />
            <div className="stack__under stack__under--near">
              {index + 1 < questions.length && <span className="stack__peek label">{t("next_sheet", { n: pad(index + 2) })}</span>}
            </div>
            <section className="sheet" aria-labelledby="prompt">
              {watching && <Watermark me={me} />}
              <RecordRow
                cells={[
                  { label: "question", numeral: true, value: <>{pad(index + 1)}<em> / {pad(questions.length)}</em></> },
                  { label: "value", value: points(current.points) },
                  { label: "type", value: t(typeKey[current.type] ?? "t_short") },
                  { label: "status", className: "record__cell--status", value: voided ? t("st_voided") : qClosed ? t("st_closed") : answered(answers[current.id]) ? t("saved") : t("unsaved") },
                ]}
              />
              {gated ? (
                <div className="timed-gate">
                  <Timer size={28} strokeWidth={2} strokeLinecap="square" />
                  <h2 className="panel__title">{t("timed_title")}</h2>
                  <p className="muted">{t("timed_body", { time: clock(limitMs) })}</p>
                  <button className="btn btn--primary" onClick={() => setOpened((o) => new Set(o).add(current.id))}>
                    {t("timed_open")}
                    <ArrowRight {...iconProps} />
                  </button>
                </div>
              ) : question ? (
                <>
                  {limitMs > 0 && closesAt > 0 && (
                    <div className={`exposure ${qClosed ? "is-closed" : ""}`} style={{ marginTop: 22 }} role="timer" aria-label={t("timed_left")}>
                      <span className="label">{t("timed_left")}</span>
                      <div className="exposure__track">
                        <div className="exposure__fill" style={{ width: `${Math.max(0, Math.min(100, (qLeft / limitMs) * 100))}%` }} />
                      </div>
                      <span className="exposure__time">{clock(qLeft)}</span>
                    </div>
                  )}
                  {question.lens && (
                    <p id="lens-hint" className="label reveal-hint lens-hint">
                      <span className="lens-hint__point">{t(isTouchOnly() ? "lens_touch" : "lens_hint")}</span>
                      <span className="lens-hint__keys">{t("lens_keys")}</span>
                    </p>
                  )}
                  <div id="prompt" className={question.prompt.length <= 140 && !question.prompt.includes("\n\n") ? "prompt" : "prompt prompt--long"}>
                    <Lens
                      key={current.id}
                      on={question.lens}
                      autoPlay={!!proc.auto_read && !autoSeen.current.has(current.id)}
                      autoEnabled={!!proc.auto_read}
                      answerHint={answerHint}
                      hold={paused || answering}
                      readSpeed={proc.read_speed}
                      initialPart={seenParts.current.get(current.id)}
                      onPartChange={(part) => seenParts.current.set(current.id, part)}
                      describedBy="lens-hint"
                      onAutoPlayed={() => autoSeen.current.add(current.id)}
                    >
                      <Markdown text={question.prompt} />
                    </Lens>
                  </div>
                  {voided && (
                    <div className="sheet-note" role="note">
                      <b>{t("voided_title")}</b>
                      <span>{t("voided_body")}</span>
                    </div>
                  )}
                  {qClosed && !voided && (
                    <div className="sheet-note" role="note">
                      <b>{t("st_closed")}</b>
                      <span>{t("timed_closed")}</span>
                    </div>
                  )}
                  <div
                    onPointerDownCapture={() => setAnswering(true)}
                    onFocusCapture={() => setAnswering(true)}
                    onBlurCapture={(e) => {
                      const next = e.relatedTarget as Node | null;
                      if (!next || !e.currentTarget.contains(next)) setAnswering(false);
                    }}
                  >
                    <Answer question={question} value={answers[current.id]} disabled={locked} onChange={change} sealKey={proc.key} hideHint={question.lens} />
                  </div>
                </>
              ) : (
                <p className="muted" style={{ marginTop: 30 }}>{t("loading")}</p>
              )}
              <div className="actions">
                <button className="btn" onClick={() => go(index - 1)} disabled={index === 0}>
                  <ArrowLeft {...iconProps} />
                  {t("prev")}
                </button>
                <button className="btn" aria-pressed={flags.has(current.id)} onClick={toggleFlag}>
                  <i className="flag-chip" />
                  {flags.has(current.id) ? t("flagged") : t("flag")}
                </button>
                {index + 1 < questions.length ? (
                  <button className="btn btn--primary" onClick={() => go(index + 1)}>
                    {t("next")}
                    <ArrowRight {...iconProps} />
                  </button>
                ) : (
                  <button className="btn btn--primary" onClick={() => setView("review")}>
                    {t("review_submit")}
                    <ArrowRight {...iconProps} />
                  </button>
                )}
              </div>
            </section>
          </div>
        ) : (
          <section className="sheet" style={{ maxWidth: 820, margin: "0 auto", paddingTop: 28 }}>
            <h1 className="prompt" style={{ marginTop: 0 }}>{t("review_title")}</h1>
            <table className="content" style={{ width: "100%", maxWidth: "none" }}>
              <thead>
                <tr>
                  <th>Nº</th>
                  <th>{t("status")}</th>
                  <th>{t("type")}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {questions.map((x, i) => {
                  const s = stateOf(x.id, -1);
                  return (
                    <tr key={x.id}>
                      <td style={{ font: "800 21px/1 var(--font-display)" }}>{pad(i + 1)}</td>
                      <td>
                        {s.key === "st_pending" ? (
                          <span className="chip chip--empty">{t("unsaved")}</span>
                        ) : s.key === "st_flagged" ? (
                          <span className="chip chip--flag"><span>{t(s.key)}</span></span>
                        ) : (
                          <span className="chip">{t(s.key)}</span>
                        )}
                      </td>
                      <td className="muted">{t(typeKey[x.type] ?? "t_short")}</td>
                      <td style={{ textAlign: "right" }}>
                        <button className="btn btn--sm" onClick={() => go(i)}>{t("review_go")}</button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <div className="actions" style={{ display: "flex" }}>
              <button className="btn" onClick={() => setView("question")}>
                <ArrowLeft {...iconProps} />
                {t("back_to_exam")}
              </button>
              <button className="btn btn--primary" onClick={() => setConfirmOpen(true)} disabled={submitting}>
                {t("submit_exam")}
              </button>
            </div>
          </section>
        )}
      </main>

      <aside className="aside">
        <span className="label">{t("legend")}</span>
        <ul className="legend" style={{ marginTop: 12 }}>
          <li><i className="swatch swatch--done" />{t("st_answered")}</li>
          <li><i className="swatch swatch--flag" />{t("st_flagged")}</li>
          <li><i className="swatch" />{t("st_pending")}</li>
          <li><i className="swatch swatch--answer" />{t("your_answer")}</li>
        </ul>
        <div className="submit-block" role="button" tabIndex={0} onClick={() => setView("review")} onKeyDown={(e) => e.key === "Enter" && setView("review")}>
          <b>{t("review_submit")}</b>
          <span>
            {t("review_hint")}
            <ArrowRight {...iconProps} />
          </span>
        </div>
      </aside>

      <nav className="bar mobile-only" style={{ display: undefined }}>
        <button className="btn" aria-label={t("index")} onClick={() => setIndexOpen(true)}>
          <List {...iconProps} />
        </button>
        <button className="btn" onClick={() => (view === "review" ? setView("question") : go(index - 1))} disabled={view === "question" && index === 0}>
          {t("prev")}
        </button>
        {view === "question" && index + 1 < questions.length ? (
          <button className="btn btn--primary" onClick={() => go(index + 1)}>
            {t("next")}
            <ArrowRight {...iconProps} />
          </button>
        ) : (
          <button className="btn btn--primary" onClick={() => (view === "review" ? setConfirmOpen(true) : setView("review"))}>
            {view === "review" ? t("submit_exam") : t("review_submit")}
          </button>
        )}
      </nav>

      <Dialog.Root open={indexOpen} onOpenChange={setIndexOpen}>
        <Dialog.Portal container={portal()}>
          <Dialog.Overlay className="dialog-overlay" />
          <div className="dialog-wrap" style={{ placeItems: "end stretch", padding: 0 }}>
            <Dialog.Content className="sheet-index dialog" style={{ width: "100%" }}>
              <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "14px 16px", borderBottom: "1px solid var(--rule-strong)" }}>
                <Dialog.Title className="label" style={{ color: "var(--ink)" }}>{t("index")} · {counts.done}/{questions.length}</Dialog.Title>
                <Dialog.Close className="btn btn--icon btn--sm" aria-label={t("close")}><X {...iconProps} /></Dialog.Close>
              </header>
              <div style={{ padding: "0 16px 12px", maxHeight: "60dvh", overflow: "auto" }}>{rail}</div>
              <div style={{ padding: "0 16px 16px" }}>
                <SaveStatus state={saveKind as "saved"} waiting={Number(waitingStr)} />
              </div>
            </Dialog.Content>
          </div>
        </Dialog.Portal>
      </Dialog.Root>

      <Dialog.Root open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog.Portal container={portal()}>
          <Dialog.Overlay className="dialog-overlay" />
          <div className="dialog-wrap">
            <Dialog.Content className="dialog" role="alertdialog">
              <div className="dialog__head"><Dialog.Title className="dialog__title">{t("confirm_title")}</Dialog.Title></div>
              <div className="dialog__body">
                <Dialog.Description>{t("confirm_body")}</Dialog.Description>
                <div className="tally">
                  <div><span className="label">{t("confirm_answered")}</span><b>{counts.done}</b></div>
                  <div><span className="label">{t("confirm_empty")}</span><b>{counts.empty}</b></div>
                  <div><span className="label">{t("confirm_flagged")}</span><b>{counts.flagged}</b></div>
                </div>
              </div>
              <div className="dialog__foot">
                <Dialog.Close className="btn">{t("keep_reviewing")}</Dialog.Close>
                <button className="btn btn--primary" onClick={() => void submit()} disabled={submitting}>{t("submit_now")}</button>
              </div>
            </Dialog.Content>
          </div>
        </Dialog.Portal>
      </Dialog.Root>

      {timeUp && (
        <div className="overlay" style={{ position: "fixed", zIndex: 40 }}>
          <div className="panel" role="alert">
            <p className="panel__title">{t("time_up_title")}</p>
            <p className="muted">{t("time_up_body")}</p>
          </div>
        </div>
      )}

      {away && !timeUp && <Curtain away={away} me={me} />}

      <Toasts toasts={toasts} onClose={(id) => setToasts((all) => all.filter((x) => x.id !== id))} />
    </div>
  );
}
