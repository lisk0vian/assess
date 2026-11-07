import type { Response } from "./grading";
import type { MeView, QuestionView } from "./types";

export type { Response };

/** ApiError carries the server's error code ("time_up", "already_joined", …). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
  /** Network failures and server errors are worth retrying; 4xx are not. */
  get retryable(): boolean {
    return this.status === 0 || this.status >= 500;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: globalThis.Response;
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, "network", "network unavailable");
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.error ?? "http", data.message ?? res.statusText);
  return data as T;
}

export interface AnswerResult {
  applied: boolean;
  server_now: number;
}

export const api = {
  me: () => request<MeView>("GET", "/api/me"),
  join: (code: string, name: string, studentID: string) =>
    request<MeView>("POST", "/api/join", { code, name, student_id: studentID }),
  question: (id: string) => request<QuestionView>("GET", `/api/questions/${encodeURIComponent(id)}`),
  answer: (id: string, seq: number, response: Response) =>
    request<AnswerResult>("PUT", `/api/answers/${encodeURIComponent(id)}`, { seq, response }),
  submit: () => request<MeView>("POST", "/api/submit"),
  pledge: () => request<MeView>("POST", "/api/pledge"),
  /** Polls a pending device approval: 202 while waiting, MeView once approved. */
  joinInfo: (code: string) => request<{ roster_only: boolean }>("GET", "/api/join/info?code=" + encodeURIComponent(code)),
  joinStatus: () => request<MeView | { status: "pending" }>("GET", "/api/join/status"),
};

/** Image paths in exam Markdown ("assets/x.png") are served under /api/assets. */
export function assetURL(src: string): string {
  return src.startsWith("assets/") ? `/api/${src}` : src;
}
