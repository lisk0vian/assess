import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "../api/client";
import type { MeView } from "../api/types";
import { setLanguage } from "../i18n";

export type Phase = "loading" | "anonymous" | "ready" | "error";

/**
 * useAttempt loads the caller's attempt and keeps it fresh. A WebSocket tells
 * us when the session or attempt changes (start, extra time, end); a slow poll
 * covers the moments the socket is down. `offset` aligns the client clock to
 * the server's.
 */
export function useAttempt() {
  const [phase, setPhase] = useState<Phase>("loading");
  const [me, setMeState] = useState<MeView | null>(null);
  const [live, setLive] = useState(false);
  const offset = useRef(0);

  const setMe = useCallback((next: MeView) => {
    offset.current = next.server_now - Date.now();
    setLanguage(next.exam.language);
    setMeState(next);
    setPhase("ready");
  }, []);

  const reload = useCallback(async () => {
    try {
      setMe(await api.me());
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) setPhase("anonymous");
      else if (!(err instanceof ApiError && err.retryable)) setPhase("error");
      // Network blips keep the current view; the socket or the poll retries.
    }
  }, [setMe]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const open = me !== null && me.attempt.state === "active" && me.session.state !== "ended";

  // Live channel with exponential reconnect.
  useEffect(() => {
    if (!open) return;
    let ws: WebSocket | null = null;
    let retry: number | undefined;
    let delay = 1000;
    let stopped = false;
    const connect = () => {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${location.host}/api/ws`);
      ws.onopen = () => {
        delay = 1000;
        setLive(true);
        void reload(); // catch up on anything missed while disconnected
      };
      ws.onmessage = () => void reload();
      ws.onclose = () => {
        setLive(false);
        if (stopped) return;
        retry = window.setTimeout(connect, delay);
        delay = Math.min(delay * 2, 15_000);
      };
    };
    connect();
    return () => {
      stopped = true;
      window.clearTimeout(retry);
      ws?.close();
    };
  }, [open, reload]);

  // Fallback poll: fast in the lobby when the socket is down, slow otherwise.
  useEffect(() => {
    if (!open) return;
    const every = live ? 15_000 : me?.session.state === "lobby" ? 2000 : 5000;
    const id = window.setInterval(() => void reload(), every);
    return () => window.clearInterval(id);
  }, [open, live, me?.session.state, reload]);

  /** Server time now, in Unix ms. */
  const now = useCallback(() => Date.now() + offset.current, []);

  return { phase, me, setMe, reload, now, live };
}
