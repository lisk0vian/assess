import { Check } from "lucide-react";
import type { MeView } from "../api/types";
import { t } from "../i18n";
import { Markdown } from "../components/Markdown";
import { iconProps, RecordRow, Wordmark } from "../components/ui";

export function Lobby({ me }: { me: MeView }) {
  const total = me.questions.reduce((s, q) => s + q.points, 0);
  return (
    <main className="solo">
      <section className="sheet" aria-live="polite">
        <Wordmark />
        <h1 style={{ font: "800 56px/1 var(--font-display)", letterSpacing: ".04em", textTransform: "uppercase" }}>{t("lobby_title")}</h1>
        <p className="muted">{t("lobby_body")}</p>
        <p className="save" style={{ marginTop: 12 }}>
          <Check {...iconProps} />
          {t("connected_as", { name: me.student.name })}
        </p>
        <div style={{ marginTop: 22, borderTop: "1px solid var(--rule-strong)" }}>
          <RecordRow
            cells={[
              { label: "exam", value: me.exam.title },
              { label: "questions", value: me.questions.length },
              { label: "points", value: total },
            ]}
          />
        </div>
        {me.exam.instructions && (
          <div style={{ marginTop: 22 }}>
            <Markdown text={me.exam.instructions} />
          </div>
        )}
      </section>
    </main>
  );
}
