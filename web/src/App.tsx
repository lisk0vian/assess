import { useEffect, useState } from "react";
import { Layers, Pause } from "lucide-react";
import { api } from "./api/client";
import { Toasts, type Toast } from "./components/ui";
import { useAttempt } from "./state/useAttempt";
import { useSingleTab } from "./state/useSingleTab";
import { t } from "./i18n";
import { Join } from "./screens/Join";
import { Lobby } from "./screens/Lobby";
import { Exam } from "./screens/Exam";
import { Result } from "./screens/Result";
import { DeviceBlock, Pledge, VerdictPanel, warningText } from "./proctor/ProctorUI";
import { telemetry } from "./proctor/telemetry";
import { fullscreenSupported, isTouchOnly } from "./proctor/useProctor";
import { uid } from "./uid";

export function App() {
  const { phase, me, setMe, now, paused, messages, dismiss, reportAt, verdict, clearVerdict } = useAttempt();
  const blocked = useSingleTab();
  const [warnings, setWarnings] = useState<Toast[]>([]);

  // The second tab tells the teacher it exists, once.
  const signed = !!me?.proctoring.key;
  useEffect(() => {
    if (blocked && signed) telemetry.report("second_tab");
  }, [blocked, signed]);

  // Verdicts that cost nothing are a quiet notice; strikes get a panel.
  useEffect(() => {
    if (!verdict || !me || verdict.strike || verdict.annulled) return;
    const body = warningText(verdict, me);
    clearVerdict();
    if (!body) return;
    setWarnings((w) => [...w.filter((x) => x.body !== body), { id: uid(), kind: "inverse", title: t("warning_title"), body }]);
  }, [verdict, me, clearVerdict]);

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
  const toasts: Toast[] = [...messages.map((m) => ({ id: m.id, kind: "plain" as const, title: t(m.all ? "msg_title_all" : "msg_title"), body: m.text })), ...warnings];
  const close = (id: string) => {
    dismiss(id);
    setWarnings((w) => w.filter((x) => x.id !== id));
  };
  return (
    <>
      {screen()}
      <Toasts toasts={toasts} onClose={close} />
      {verdict?.strike && !verdict.annulled && (
        <VerdictPanel
          verdict={verdict}
          fullscreen={me?.proctoring.fullscreen === "required" && fullscreenSupported() && !document.fullscreenElement}
          onClose={clearVerdict}
        />
      )}
      {paused && (
        <div className="overlay" style={{ position: "fixed", zIndex: 45 }}>
          <section className="panel panel--inverse" role="alert">
            <Pause size={28} strokeWidth={2} strokeLinecap="square" />
            <h1 className="panel__title">{t("pause_title")}</h1>
            <p>{t(paused === "all" ? "pause_body_all" : "pause_body")}</p>
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
    const p = me.proctoring;
    if (p.devices === "desktop" && isTouchOnly()) return <DeviceBlock />;
    if (p.key && !p.pledged) return <Pledge me={me} onPledge={async () => setMe(await api.pledge())} />;
    if (me.session.state === "lobby") return <Lobby me={me} />;
    return <Exam me={me} now={now} setMe={setMe} reportAt={reportAt} paused={!!paused} />;
  }
}
