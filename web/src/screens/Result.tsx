import type { MeView } from "../api/types";
import { t } from "../i18n";
import { Wordmark } from "../components/ui";

const fmt = (n: number) => n.toLocaleString(document.documentElement.lang, { maximumFractionDigits: 2 });

export function Result({ me }: { me: MeView }) {
  const a = me.attempt;
  if (a.state === "annulled" || a.state === "withdrawn") {
    const annulled = a.state === "annulled";
    return (
      <main className="solo" style={{ background: "var(--inverse)" }}>
        <section className="panel panel--inverse" style={{ border: 0, boxShadow: "none" }}>
          <h1 className="panel__title" style={{ fontSize: 44 }}>{t(annulled ? "annulled_title" : "withdrawn_title")}</h1>
          <p>{t(annulled ? "annulled_body" : "withdrawn_body")}</p>
        </section>
      </main>
    );
  }
  const at = a.submitted_at ? new Date(a.submitted_at).toLocaleTimeString(document.documentElement.lang, { hour: "2-digit", minute: "2-digit" }) : "";
  return (
    <main className="solo">
      <section className="sheet">
        <Wordmark />
        {a.score !== undefined ? (
          <p className="score" style={{ font: "800 112px/.85 var(--font-display)", margin: "26px 0 12px" }}>
            {fmt(a.score)}
            <small style={{ fontSize: 48, color: "var(--ink-3)" }}> / {fmt(a.max_score)}</small>
          </p>
        ) : (
          <h1 className="panel__title" style={{ fontSize: 44, margin: "22px 0 10px" }}>{t("result_title")}</h1>
        )}
        {at && <p>{t("result_submitted_at", { time: at })}</p>}
        {a.submit_reason === "timeout" && <p className="muted">{t("result_timeout")}</p>}
        <p className="muted">{me.exam.result_visibility === "review" && !me.review ? t("result_review_later") : t("result_body")}</p>
        {me.review && (
          <table className="tablelike" style={{ marginTop: 22, width: "100%", borderCollapse: "collapse" }}>
            <tbody>
              {me.review.map((r, i) => (
                <tr key={r.question_id} style={{ borderBottom: "1px solid var(--rule)" }}>
                  <td style={{ padding: "10px 12px 10px 0", font: "800 21px/1 var(--font-display)" }}>{String(i + 1).padStart(2, "0")}</td>
                  <td className="label" style={{ color: r.correct ? "var(--ink)" : "var(--ink-2)" }}>{r.correct ? t("correct") : t("incorrect")}</td>
                  <td style={{ textAlign: "right" }}>
                    {fmt(r.points)} / {fmt(r.max_points)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </main>
  );
}
