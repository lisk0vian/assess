import { useAttempt } from "./state/useAttempt";
import { t } from "./i18n";
import { Join } from "./screens/Join";
import { Lobby } from "./screens/Lobby";
import { Exam } from "./screens/Exam";
import { Result } from "./screens/Result";

export function App() {
  const { phase, me, setMe, now } = useAttempt();

  if (phase === "loading") return <main className="solo"><p className="muted">{t("loading")}</p></main>;
  if (phase === "error") return <main className="solo"><p className="field__error">{t("err_generic")}</p></main>;
  if (phase === "anonymous" || !me) return <Join onJoined={setMe} />;

  if (me.attempt.state !== "active" || me.session.state === "ended") return <Result me={me} />;
  if (me.session.state === "lobby") return <Lobby me={me} />;
  return <Exam me={me} now={now} setMe={setMe} />;
}
