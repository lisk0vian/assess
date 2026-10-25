import { fromHex, hmacSha256, toHex, utf8 } from "./crypto";

/** What the server decided about a signal (pushed as {"type":"verdict"}). */
export interface Verdict {
  kind: string;
  question_id?: string;
  warn: boolean;
  strike: boolean;
  lose_ms: number;
  void: boolean;
  strikes: number;
  limit: number;
  annulled: boolean;
}

export interface SignalOptions {
  phase?: "start" | "end";
  durationMs?: number;
  questionID?: string;
}

/**
 * Telemetry signs and numbers integrity signals and sends them on the live
 * socket. While the socket is down, signals wait in order and go out on
 * reconnect; the server ignores numbers it has already seen.
 */
class Telemetry {
  private key: Uint8Array | null = null;
  private seq = 0;
  private outbox: string[] = [];
  private sink: ((msg: string) => void) | null = null;
  private onAttach: (() => void) | null = null;

  /** Called with every fresh attempt state. Without a key nothing is sent. */
  configure(keyHex: string | undefined, serverSeq: number) {
    this.key = keyHex ? fromHex(keyHex) : null;
    this.seq = Math.max(this.seq, serverSeq);
    if (!this.key) this.outbox = [];
  }

  get enabled() {
    return this.key !== null;
  }

  attach(sink: (msg: string) => void) {
    this.sink = sink;
    this.onAttach?.(); // re-announce an absence in progress before anything else
    for (const m of this.outbox.splice(0)) sink(m);
  }

  /** Called on every (re)connection, so an ongoing absence is not taken as abandoned. */
  whenAttached(fn: (() => void) | null) {
    this.onAttach = fn;
  }

  detach() {
    this.sink = null;
  }

  report(kind: string, o: SignalOptions = {}) {
    if (!this.key) return;
    const seq = ++this.seq;
    const phase = o.phase ?? "";
    const duration = Math.max(0, Math.round(o.durationMs ?? 0));
    const question = o.questionID ?? "";
    const mac = toHex(hmacSha256(this.key, utf8(`${seq}|${kind}|${phase}|${duration}|${question}`)));
    const msg = JSON.stringify({ type: "signal", seq, kind, phase, duration_ms: duration, question_id: question, at: Date.now(), mac });
    if (this.sink) this.sink(msg);
    else if (this.outbox.length < 50) this.outbox.push(msg);
  }
}

export const telemetry = new Telemetry();
