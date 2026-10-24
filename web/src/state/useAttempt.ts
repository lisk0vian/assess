import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "../api/client";
import type { MeView } from "../api/types";
import { setLanguage } from "../i18n";

export type Phase = "loading" | "anonymous" | "ready" | "error";

/**
 * useAttempt loads the caller's attempt and keeps it fresh: it polls while
 * the session is open so the lobby notices the start and the exam notices
 * extra time or an early end. `offset` aligns the client clock to the server.
 */
export function useAttempt() {
  const [phase, setPhase] = useState<Phase>("loading");
  const [me, setMeState] = useState<MeView | null>(null);
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
      // Network blips keep the current view; the next poll retries.
    }
  }, [setMe]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const open = me !== null && me.attempt.state === "active" && me.session.state !== "ended";
  useEffect(() => {
    if (!open) return;
    const lobby = me?.session.state === "lobby";
    const id = window.setInterval(() => void reload(), lobby ? 2000 : 5000);
    return () => window.clearInterval(id);
  }, [open, me?.session.state, reload]);

  /** Server time now, in Unix ms. */
  const now = useCallback(() => Date.now() + offset.current, []);

  return { phase, me, setMe, reload, now };
}
