import type { ReactNode } from "react";
import { Check, Clock, LoaderCircle, WifiOff, X } from "lucide-react";
import { t, type Key } from "../i18n";
import type { SaveState } from "../state/answerQueue";

/** Lucide icons with the system's square stroke ends. */
export const iconProps = { size: 18, strokeWidth: 2, strokeLinecap: "square", strokeLinejoin: "miter" } as const;

export function Wordmark({ small = false }: { small?: boolean }) {
  return (
    <span className="wordmark" style={small ? { fontSize: 24 } : undefined}>
      ASSESS
    </span>
  );
}

export function Label({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <span className={`label ${className}`}>{children}</span>;
}

export interface RecordCell {
  label: Key;
  value: ReactNode;
  numeral?: boolean;
  className?: string;
}

export function RecordRow({ cells }: { cells: RecordCell[] }) {
  return (
    <div className="record">
      {cells.map((c) => (
        <div className={`record__cell ${c.className ?? ""}`} key={c.label}>
          <Label>{t(c.label)}</Label>
          <b className={c.numeral ? "record__num" : "record__value"}>{c.value}</b>
        </div>
      ))}
    </div>
  );
}

export function SaveStatus({ state, waiting }: { state: SaveState; waiting: number }) {
  if (state === "offline") {
    return (
      <span className="save save--offline" role="status">
        <WifiOff {...iconProps} />
        {t("save_offline", { n: waiting })}
      </span>
    );
  }
  if (state === "saving") {
    return (
      <span className="save is-saving" role="status">
        <LoaderCircle {...iconProps} />
        {t("save_saving")}
      </span>
    );
  }
  return (
    <span className="save" role="status">
      <Check {...iconProps} />
      {state === "closed" ? t("save_closed") : t("save_saved")}
    </span>
  );
}

/** Remaining time in m:ss / h:mm:ss with tabular digits. */
export function clock(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const mm = String(m).padStart(2, "0");
  const ss = String(sec).padStart(2, "0");
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

/** Escalation inverts to ink; it never turns red. */
export function timerStage(ms: number): { label: Key; className: string } {
  if (ms <= 60_000) return { label: "last_1", className: "timer--last" };
  if (ms <= 5 * 60_000) return { label: "last_5", className: "timer--urgent" };
  if (ms <= 10 * 60_000) return { label: "last_10", className: "timer--soon" };
  return { label: "time_left", className: "" };
}

export interface Toast {
  id: string;
  kind: "yours" | "plain" | "inverse";
  title: string;
  body?: string;
  action?: { label: string; run: () => void };
}

export function Toasts({ toasts, onClose }: { toasts: Toast[]; onClose: (id: string) => void }) {
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((n) => (
        <div key={n.id} className={`notice ${n.kind === "yours" ? "notice--yours" : n.kind === "inverse" ? "notice--inverse" : ""}`}>
          {n.kind === "plain" ? <Clock size={28} strokeWidth={2} strokeLinecap="square" /> : <i className="notice__mark" />}
          <div>
            <div className="notice__title">{n.title}</div>
            {n.body && <div className="notice__body">{n.body}</div>}
            {n.action && (
              <button
                className="btn btn--sm"
                style={{ marginTop: 10 }}
                onClick={() => {
                  n.action!.run();
                  onClose(n.id);
                }}
              >
                {n.action.label}
              </button>
            )}
          </div>
          <button className="btn btn--icon btn--sm btn--quiet" aria-label={t("close")} onClick={() => onClose(n.id)}>
            <X {...iconProps} />
          </button>
        </div>
      ))}
    </div>
  );
}
