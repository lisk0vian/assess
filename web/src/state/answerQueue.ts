import { api, ApiError, type Response } from "../api/client";

export type SaveState = "saved" | "saving" | "offline" | "closed";

interface Pending {
  seq: number;
  response: Response;
}

/**
 * AnswerQueue sends answer changes in order and survives network drops.
 * Every change gets a higher sequence number, so retries and late deliveries
 * never overwrite a newer answer on the server. Only the latest change per
 * question is kept while offline.
 */
export class AnswerQueue {
  private seq: number;
  private pending = new Map<string, Pending>();
  private flushing = false;
  private backoff = 1000;
  private timer: number | undefined;
  private listeners = new Set<() => void>();

  state: SaveState = "saved";
  lastSavedAt = 0;

  constructor(lastSeq: number) {
    this.seq = lastSeq;
  }

  get waiting(): number {
    return this.pending.size;
  }

  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private emit(state: SaveState) {
    this.state = state;
    this.listeners.forEach((fn) => fn());
  }

  /** Raise the sequence floor (e.g. after reloading the attempt). */
  sync(serverLastSeq: number) {
    this.seq = Math.max(this.seq, serverLastSeq);
  }

  put(questionID: string, response: Response) {
    if (this.state === "closed") return;
    this.seq += 1;
    this.pending.set(questionID, { seq: this.seq, response });
    void this.flush();
  }

  close() {
    window.clearTimeout(this.timer);
    this.pending.clear();
    this.emit("closed");
  }

  private async flush() {
    if (this.flushing || this.state === "closed") return;
    this.flushing = true;
    this.emit("saving");
    try {
      while (this.pending.size > 0) {
        const [qid, item] = this.pending.entries().next().value as [string, Pending];
        try {
          await api.answer(qid, item.seq, item.response);
          if (this.pending.get(qid)?.seq === item.seq) this.pending.delete(qid);
          this.backoff = 1000;
          this.lastSavedAt = Date.now();
        } catch (err) {
          if (err instanceof ApiError && err.retryable) {
            this.emit("offline");
            window.clearTimeout(this.timer);
            this.timer = window.setTimeout(() => void this.flush(), this.backoff);
            this.backoff = Math.min(this.backoff * 2, 10_000);
            return;
          }
          if (err instanceof ApiError && ["time_up", "attempt_closed", "session_closed"].includes(err.code)) {
            this.close();
            return;
          }
          // A rejected change (invalid shape) is dropped; keep the rest.
          this.pending.delete(qid);
        }
      }
      this.emit("saved");
    } finally {
      this.flushing = false;
    }
  }
}
