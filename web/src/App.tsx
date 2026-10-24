import { Layers } from "lucide-react";
import { useAttempt } from "./state/useAttempt";
import { useSingleTab } from "./state/useSingleTab";
import { t } from "./i18n";
import { Join } from "./screens/Join";
import { Lobby } from "./screens/Lobby";
import { Exam } from "./screens/Exam";
import { Result } from "./screens/Result";

export function App() {
  const { phase, me, setMe, now } = useAttempt();
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
  if (phase === "loading") return <main className="solo"><p className="muted">{t("loading")}</p></main>;
  if (phase === "error") return <main className="solo"><p className="field__error">{t("err_generic")}</p></main>;
  if (phase === "anonymous" || !me) return <Join onJoined={setMe} />;

  if (me.attempt.state !== "active" || me.session.state === "ended") return <Result me={me} />;
  if (me.session.state === "lobby") return <Lobby me={me} />;
  return <Exam me={me} now={now} setMe={setMe} />;
}
