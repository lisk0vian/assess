import { useEffect, useMemo, useState, type ClipboardEvent, type DragEvent } from "react";
import { EyeOff, Maximize, Monitor } from "lucide-react";
import type { MeView } from "../api/types";
import { t } from "../i18n";
import { clock, iconProps, Wordmark } from "../components/ui";
import type { Verdict } from "./telemetry";
import { fullscreenSupported, type Away } from "./useProctor";

/** Strike boxes; the counter is always visible while proctoring is on. */
export function Strikes({ n, limit, compact = false }: { n: number; limit: number; compact?: boolean }) {
  const text = compact ? `${n}/${limit}` : t(n === limit - 1 && n > 0 ? "strikes_last" : "strikes_of", { n, limit });
  return (
    <div className="strikes" role="status" aria-label={`${t("warnings")}: ${t("strikes_of", { n, limit })}`}>
      {Array.from({ length: limit }, (_, i) => (
        <i key={i} className={i < n ? "on" : ""} />
      ))}
      <span aria-hidden="true">{text}</span>
    </div>
  );
}

/** Name, ID and time across the sheet, so a photo of the screen says whose it is. */
export function Watermark({ me }: { me: MeView }) {
  const [time, setTime] = useState(() => hhmm());
  useEffect(() => {
    const id = window.setInterval(() => setTime(hhmm()), 30_000);
    return () => window.clearInterval(id);
  }, []);
  const text = `${me.student.name} · ${me.student.student_id} · ${time}`.toUpperCase();
  return (
    <div className="watermark" aria-hidden="true">
      <svg>
        <defs>
          <pattern id="wm" width="420" height="240" patternUnits="userSpaceOnUse" patternTransform="rotate(-18)">
            <text x="0" y="120" fill="currentColor" fontFamily="var(--font-display)" fontWeight="700" fontSize="15" letterSpacing="2">
              {text}
            </text>
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#wm)" />
      </svg>
    </div>
  );
}

export const hhmm = () => new Date().toLocaleTimeString(document.documentElement.lang, { hour: "2-digit", minute: "2-digit" });

const enterFullscreen = () => {
  if (fullscreenSupported() && !document.fullscreenElement) void document.documentElement.requestFullscreen().catch(() => undefined);
};

/**
 * Curtain covers the exam the moment it is not in view. After the warning
 * threshold it turns into a countdown to the strike, mirrored in the tab title.
 */
export function Curtain({ away, me }: { away: Away; me: MeView }) {
  const th = me.proctoring.thresholds;
  const strikeAfter = away.kind === "hidden" ? th.hidden_strike_ms : away.kind === "blur" ? th.blur_strike_ms : th.fullscreen_strike_ms;
  const warnAfter = away.kind === "hidden" ? th.hidden_warn_ms : away.kind === "blur" ? th.blur_warn_ms : 0;
  const [elapsed, setElapsed] = useState(() => performance.now() - away.since);
  useEffect(() => {
    const tick = () => setElapsed(performance.now() - away.since);
    tick();
    const id = window.setInterval(tick, 250);
    return () => window.clearInterval(id);
  }, [away.since]);

  const left = strikeAfter - elapsed;
  const counting = elapsed >= warnAfter;
  useEffect(() => {
    const original = document.title;
    return () => {
      document.title = original;
    };
  }, []);
  useEffect(() => {
    document.title = counting ? (left > 0 ? `${clock(left)} · ${t("away_tab")}` : t("away_tab")) : document.title;
  }, [counting, left]);

  const next = { n: me.proctoring.strikes + 1, limit: me.proctoring.strike_limit };
  if (away.kind === "fullscreen_exit") {
    return (
      <div className="overlay curtain" role="alertdialog" aria-labelledby="curtain-title">
        <section className="panel">
          <Maximize size={28} strokeWidth={2} strokeLinecap="square" />
          <h1 className="panel__title" id="curtain-title">{t("fs_title")}</h1>
          <p className="muted">{t("fs_body", { s: Math.round(strikeAfter / 1000) })}</p>
          <p className="panel__count" aria-live="off">{left > 0 ? clock(left) : "0:00"}</p>
          <p className="muted">{left > 0 ? t("away_body", next) : t("away_over")}</p>
          <div className="actions">
            <button className="btn btn--primary" onClick={enterFullscreen} autoFocus>
              <Maximize {...iconProps} />
              {t("fs_enter")}
            </button>
          </div>
        </section>
      </div>
    );
  }
  if (!counting) {
    return (
      <div className="overlay curtain" role="alert">
        <section className="panel" style={{ textAlign: "center" }}>
          <EyeOff size={28} strokeWidth={2} strokeLinecap="square" />
          <h1 className="panel__title">{t("curtain_title")}</h1>
          <p className="muted">{t("curtain_body")}</p>
        </section>
      </div>
    );
  }
  return (
    <div className="overlay curtain curtain--count" role="alert">
      <section className="panel panel--inverse">
        <h1 className="panel__title">{t("away_title")}</h1>
        <p className="panel__count" aria-live="off">{left > 0 ? clock(left) : "0:00"}</p>
        <p>{left > 0 ? t("away_body", next) : t("away_over")}</p>
      </section>
    </div>
  );
}

const verdictKinds = new Set(["hidden", "blur", "fullscreen_exit", "copy", "paste_input", "untrusted", "webdriver", "telemetry"]);

/** Shown on return when a signal cost a strike. */
export function VerdictPanel({ verdict, onClose }: { verdict: Verdict; onClose: () => void }) {
  const reason = verdictKinds.has(verdict.kind) ? (`verdict_${verdict.kind}` as const) : "verdict_generic";
  return (
    <div className="overlay" style={{ position: "fixed", zIndex: 44 }}>
      <section className="panel" role="alertdialog" aria-labelledby="verdict-title" aria-describedby="verdict-body">
        <h1 className="panel__title" id="verdict-title">{t("verdict_title", { n: verdict.strikes, limit: verdict.limit })}</h1>
        <div id="verdict-body" className="muted">
          <p>
            {t(reason as Parameters<typeof t>[0])}
            {verdict.lose_ms > 0 && <> {t("verdict_lost", { n: Math.round(verdict.lose_ms / 60_000) })}</>}
            {verdict.void && <> {t("verdict_void")}</>}
          </p>
          <p>{t("verdict_revert")}</p>
        </div>
        <div style={{ marginTop: 14 }}>
          <Strikes n={verdict.strikes} limit={verdict.limit} />
        </div>
        <div className="actions">
          <button className="btn btn--primary" onClick={onClose} autoFocus>
            {t("verdict_ok")}
          </button>
        </div>
      </section>
    </div>
  );
}

/** The non-blocking text for a warning-only verdict, or null when there is nothing to say. */
export function warningText(v: Verdict, me: MeView): string | null {
  const th = me.proctoring.thresholds;
  switch (v.kind) {
    case "hidden":
      return t("warning_hidden", { s: Math.round(th.hidden_strike_ms / 1000) });
    case "blur":
      return t("warning_blur", { s: Math.round(th.blur_strike_ms / 1000) });
    case "copy":
      return t("warning_copy");
    case "paste_input":
      return t("warning_paste_input");
    default:
      return v.warn ? t("warning_generic") : null;
  }
}

export function DeviceBlock() {
  return (
    <main className="solo">
      <section className="panel" role="alert">
        <Monitor size={28} strokeWidth={2} strokeLinecap="square" />
        <h1 className="panel__title">{t("device_title")}</h1>
        <p className="muted">{t("device_body")}</p>
      </section>
    </main>
  );
}

const fold = (s: string) =>
  s
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();

/**
 * Pledge lists what is recorded and its consequence, then asks the student
 * to type the honour pledge. Starting is also the gesture fullscreen needs.
 */
export function Pledge({ me, onPledge }: { me: MeView; onPledge: (fullscreen: boolean) => Promise<void> }) {
  const p = me.proctoring;
  const th = p.thresholds;
  const strict = p.profile === "strict";
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const ok = fold(typed) === fold(t("pledge_text"));
  const fsOffer = p.fullscreen !== "off" && fullscreenSupported();
  const sec = (ms: number) => Math.round(ms / 1000);

  const rows = useMemo(() => {
    const r: [string, string][] = [
      [t("rule_hidden", { s: sec(th.hidden_strike_ms) }), t(strict ? "fx_strike_time" : "fx_strike")],
      [t("rule_blur", { s: sec(th.blur_strike_ms) }), t("fx_strike")],
      [t("rule_copy"), t(strict ? "fx_copy_strict" : "fx_copy_standard")],
      [t("rule_paste"), t(strict ? "fx_strike" : "fx_warning")],
    ];
    if (p.fullscreen === "required") r.push([t("rule_fullscreen", { s: sec(th.fullscreen_strike_ms) }), t("fx_strike")]);
    r.push([t("rule_limit", { n: p.strike_limit }), t("fx_annul")]);
    return r;
  }, [th, strict, p.fullscreen, p.strike_limit]);

  const start = async (fullscreen: boolean) => {
    if (!ok || busy) return;
    if (fullscreen) enterFullscreen(); // inside the click: browsers require a gesture
    setBusy(true);
    try {
      await onPledge(fullscreen);
    } finally {
      setBusy(false);
    }
  };
  const noPaste = (e: ClipboardEvent | DragEvent) => e.preventDefault();

  return (
    <main className="solo">
      <section className="sheet pledge">
        <Wordmark />
        <h1>{t("rules_title")}</h1>
        <table className="tablelike">
          <thead>
            <tr>
              <th>{t("rules_recorded")}</th>
              <th>{t("rules_effect")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(([what, fx]) => (
              <tr key={what}>
                <td>{what}</td>
                <td>{fx}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="field__hint" style={{ marginTop: 12 }}>{t("rules_note")}</p>
        <label className="field" style={{ marginTop: 22 }}>
          <span className="label">{t("pledge_label")}</span>
          <span className="pledge-target" aria-hidden="true">{t("pledge_text")}</span>
          <input
            className={`input input--text ${ok ? "is-answer" : ""}`}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            onPaste={noPaste}
            onDrop={noPaste}
            onKeyDown={(e) => e.key === "Enter" && void start(fsOffer && p.fullscreen === "required")}
            autoComplete="off"
            spellCheck={false}
            aria-describedby="pledge-hint"
          />
          <span className="field__hint" id="pledge-hint">{t("pledge_hint")}</span>
        </label>
        <div className="stack-y" style={{ marginTop: 18, gap: 10 }}>
          <button className="btn btn--primary btn--block" disabled={!ok || busy} onClick={() => void start(fsOffer)}>
            {fsOffer && <Maximize {...iconProps} />}
            {fsOffer && p.fullscreen === "suggested" ? t("start_fullscreen") : t("start")}
          </button>
          {fsOffer && p.fullscreen === "suggested" && (
            <button className="btn btn--block btn--quiet" disabled={!ok || busy} onClick={() => void start(false)}>
              {t("start_windowed")}
            </button>
          )}
        </div>
      </section>
    </main>
  );
}
