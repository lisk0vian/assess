import { Layers, Pause } from "lucide-react";
import { Toasts } from "./components/ui";
import { useAttempt } from "./state/useAttempt";
import { useSingleTab } from "./state/useSingleTab";
import { t } from "./i18n";
import { Join } from "./screens/Join";
import { Lobby } from "./screens/Lobby";
import { Exam } from "./screens/Exam";
import { Result } from "./screens/Result";

export function App() {
  const { phase, me, setMe, now, paused, messages, dismiss, reportAt } = useAttempt();
  const blocked = useSingleTab();

  if (blocked) {
    return (
      <main className="solo">
        <section className="panel" role="alert">
          <Layers size={28} strokeWidth={2} strokeLinecap="square" />
          <h1 className="panel__title">{t("tab_title")}</h1>
          <p className="muted">{t("tab_body")}</p>
        </section>
      </main>
    );
  }
  return (
    <>
      {screen()}
      <Toasts toasts={messages.map((m) => ({ id: m.id, kind: "plain" as const, title: t("msg_title"), body: m.text }))} onClose={dismiss} />
      {paused && (
        <div className="overlay" style={{ position: "fixed", zIndex: 45 }}>
          <section className="panel panel--inverse" role="alert">
            <Pause size={28} strokeWidth={2} strokeLinecap="square" />
            <h1 className="panel__title">{t("pause_title")}</h1>
            <p>{t("pause_body")}</p>
          </section>
        </div>
      )}
    </>
  );

  // A plain function, not a component: a nested component would remount the
  // exam on every render and lose its state.
  function screen() {
  if (phase === "loading") return <main className="solo"><p className="muted">{t("loading")}</p></main>;
  if (phase === "error") return <main className="solo"><p className="field__error">{t("err_generic")}</p></main>;
  if (phase === "anonymous" || !me) return <Join onJoined={setMe} />;

  if (me.attempt.state !== "active" || me.session.state === "ended") return <Result me={me} />;
  if (me.session.state === "lobby") return <Lobby me={me} />;
  return <Exam me={me} now={now} setMe={setMe} reportAt={reportAt} />;
  }
}
