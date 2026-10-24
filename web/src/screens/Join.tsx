import { useState, type FormEvent } from "react";
import { ArrowRight, Info } from "lucide-react";
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
};

export function Join({ onJoined }: { onJoined: (me: MeView) => void }) {
  const fromLink = new URLSearchParams(location.search).get("c") ?? "";
  const [code, setCode] = useState(fromLink.toUpperCase());
  const [name, setName] = useState("");
  const [studentID, setStudentID] = useState("");
  const [error, setError] = useState<Key | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onJoined(await api.join(code.trim(), name.trim(), studentID.trim()));
    } catch (err) {
      setError(err instanceof ApiError ? (errorKeys[err.code] ?? "err_generic") : "err_generic");
    } finally {
      setBusy(false);
    }
  };

  const ready = code.trim().length === 6 && name.trim() && studentID.trim();
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
          <label className="field">
            <span className="label">{t("join_name")}</span>
            <input className="input input--text" value={name} onChange={(e) => setName(e.target.value)} maxLength={100} autoComplete="name" autoFocus={!!fromLink} />
            <span className="field__hint">{t("join_name_hint")}</span>
          </label>
          <label className={`field ${error === "err_already_joined" ? "is-error" : ""}`}>
            <span className="label">{t("join_id")}</span>
            <input className="input input--text" value={studentID} onChange={(e) => setStudentID(e.target.value)} maxLength={64} autoComplete="off" />
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
