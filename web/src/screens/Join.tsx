import { useEffect, useState, type FormEvent } from "react";
import { ArrowRight, Info, LoaderCircle, Monitor } from "lucide-react";
import { api, ApiError } from "../api/client";
import type { MeView } from "../api/types";
import { t, type Key } from "../i18n";
import { iconProps, Wordmark } from "../components/ui";

const errorKeys: Record<string, Key> = {
  bad_code: "err_bad_code",
  already_joined: "err_already_joined",
  session_closed: "err_session_closed",
  invalid_input: "err_invalid_input",
  network: "err_network",
  join_closed: "err_join_closed",
  too_many_attempts: "err_too_many",
  outside_network: "err_network",
  rejected: "err_rejected",
  name_required: "err_name_required",
};

export function Join({ onJoined }: { onJoined: (me: MeView) => void }) {
  const fromLink = new URLSearchParams(location.search).get("c") ?? "";
  const [code, setCode] = useState(fromLink.toUpperCase());
  const [name, setName] = useState("");
  const [studentID, setStudentID] = useState("");
  const [error, setError] = useState<Key | null>(null);
  const [busy, setBusy] = useState(false);
  const [waiting, setWaiting] = useState(false);
  // A session that lets in only its class list knows every name: the ID is enough,
  // unless the ID is not on the list.
  const [idOnly, setIdOnly] = useState(false);
  const [needName, setNeedName] = useState(false);
  const askName = !idOnly || needName;

  useEffect(() => {
    const c = code.trim();
    setIdOnly(false);
    if (c.length !== 6) return;
    let live = true;
    api.joinInfo(c).then((info) => live && setIdOnly(info.roster_only), () => {});
    return () => {
      live = false;
    };
  }, [code]);

  // While the teacher decides, poll the request every two seconds.
  useEffect(() => {
    if (!waiting) return;
    const id = window.setInterval(async () => {
      try {
        const res = await api.joinStatus();
        if ("exam" in res) onJoined(res);
      } catch (err) {
        if (err instanceof ApiError && !err.retryable) {
          setWaiting(false);
          setError(errorKeys[err.code] ?? "err_generic");
        }
      }
    }, 2000);
    return () => window.clearInterval(id);
  }, [waiting, onJoined]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onJoined(await api.join(code.trim(), askName ? name.trim() : "", studentID.trim()));
    } catch (err) {
      if (err instanceof ApiError && err.code === "pending_approval") setWaiting(true);
      else if (err instanceof ApiError && err.code === "name_required") {
        setNeedName(true);
        setError("err_name_required");
      }
      else setError(err instanceof ApiError ? (errorKeys[err.code] ?? "err_generic") : "err_generic");
    } finally {
      setBusy(false);
    }
  };

  if (waiting) {
    return (
      <main className="solo">
        <section className="panel" aria-live="polite">
          <Monitor size={28} strokeWidth={2} strokeLinecap="square" />
          <h1 className="panel__title">{t("wait_title")}</h1>
          <p className="muted">{t("wait_body")}</p>
          <p className="save is-saving" style={{ marginTop: 14 }}>
            <LoaderCircle {...iconProps} />
            {askName && name.trim() ? `${name} · ${studentID}` : studentID}
          </p>
        </section>
      </main>
    );
  }

  const ready = code.trim().length === 6 && (!askName || name.trim()) && studentID.trim();
  return (
    <main className="solo">
      <form className="sheet" onSubmit={submit} noValidate>
        <Wordmark />
        <h1>{t("join_title")}</h1>
        <div className="stack-y">
          <label className="field">
            <span className="label">{t("join_code")}</span>
            <input
              className="input code-input"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/[^a-z0-9]/gi, "").toUpperCase())}
              maxLength={6}
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
              aria-describedby="code-hint"
              autoFocus={!fromLink}
            />
            <span className="field__hint" id="code-hint">{t("join_code_hint")}</span>
          </label>
          {askName && (
            <label className={`field ${error === "err_name_required" ? "is-error" : ""}`}>
              <span className="label">{t("join_name")}</span>
              <input className="input input--text" value={name} onChange={(e) => setName(e.target.value)} maxLength={100} autoComplete="name" autoFocus={!!fromLink || needName} />
              <span className="field__hint">{t("join_name_hint")}</span>
            </label>
          )}
          <label className={`field ${error === "err_already_joined" ? "is-error" : ""}`}>
            <span className="label">{t("join_id")}</span>
            <input className="input input--text" value={studentID} onChange={(e) => setStudentID(e.target.value)} maxLength={64} autoComplete="off" autoFocus={!!fromLink && idOnly} />
            {!askName && <span className="field__hint">{t("join_id_only_hint")}</span>}
          </label>
          {error && (
            <p className="field__error" role="alert">
              <Info {...iconProps} />
              {t(error)}
            </p>
          )}
          <button className="btn btn--primary btn--block" disabled={!ready || busy}>
            {t("join_submit")}
            <ArrowRight {...iconProps} />
          </button>
        </div>
      </form>
    </main>
  );
}
