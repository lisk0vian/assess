import { useEffect, useState } from "react";

const CHANNEL = "assess-exam-tab";

/**
 * useSingleTab reports whether another tab of this browser already has the
 * exam open. Classroom servers run plain HTTP, where the Web Locks API is not
 * available, so tabs negotiate over BroadcastChannel: a newcomer asks, and
 * the tab that holds the exam answers.
 */
export function useSingleTab(): boolean {
  const [blocked, setBlocked] = useState(false);
  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;
    const ch = new BroadcastChannel(CHANNEL);
    let holder = false;
    const timer = window.setTimeout(() => {
      holder = true;
    }, 350);
    ch.onmessage = (e: MessageEvent<string>) => {
      if (e.data === "who" && holder) ch.postMessage("taken");
      if (e.data === "taken" && !holder) {
        window.clearTimeout(timer);
        setBlocked(true);
      }
    };
    ch.postMessage("who");
    return () => {
      window.clearTimeout(timer);
      ch.close();
    };
  }, []);
  return blocked;
}
