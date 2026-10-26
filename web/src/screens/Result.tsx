import { Fragment, useEffect } from "react";
import type { Response } from "../api/grading";
import type { KeyView, MeView, OptionView, ReviewItem } from "../api/types";
import { Markdown } from "../components/Markdown";
import { Wordmark } from "../components/ui";
import { t } from "../i18n";

const fmt = (n: number) => n.toLocaleString(document.documentElement.lang, { maximumFractionDigits: 2 });
const bool = (v: boolean) => t(v ? "true" : "false");

function optionText(id: string, options?: OptionView[]): string {
  return options?.find((o) => o.id === id)?.text ?? id;
}

function optionList(ids: string[] | undefined, options?: OptionView[]): string {
  return (ids ?? []).map((id) => optionText(id, options)).join(", ");
}

function givenText(r: ReviewItem, answer?: Response): string {
  if (!answer) return t("unsaved");
  if (answer.option) return optionText(answer.option, r.options);
  if (answer.options?.length) return optionList(answer.options, r.options);
  if (answer.value !== undefined) return bool(answer.value);
  if (answer.text) return answer.text;
  return t("unsaved");
}

function correctText(r: ReviewItem): string {
  const k: KeyView = r.key;
  if (k.option) return optionText(k.option, r.options);
  if (k.options?.length) return optionList(k.options, r.options);
  if (k.value !== undefined) return bool(k.value);
  if (k.number !== undefined) return fmt(k.number);
  return k.accept?.[0] ?? "";
}

export function Result({ me }: { me: MeView }) {
  const a = me.attempt;
  // The exam is over: give the screen back.
  useEffect(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
  }, []);
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
        {a.submit_reason === "ended" && <p className="muted">{t("result_ended")}</p>}
        {!me.review && (
          <p className="muted">{me.exam.result_visibility === "review" ? t("result_review_later") : t("result_body")}</p>
        )}
        {me.review && (
          <table className="tablelike" style={{ marginTop: 22, width: "100%", borderCollapse: "collapse" }}>
            <tbody>
              {me.review.map((r, i) => (
                <Fragment key={r.question_id}>
                  <tr style={{ borderBottom: r.correct ? "1px solid var(--rule)" : undefined }}>
                    <td style={{ padding: "10px 12px 10px 0", font: "800 21px/1 var(--font-display)" }}>{String(i + 1).padStart(2, "0")}</td>
                    <td className="label" style={{ color: r.correct ? "var(--ink)" : "var(--ink-2)" }}>{r.correct ? t("correct") : t("incorrect")}</td>
                    <td style={{ textAlign: "right" }}>
                      {fmt(r.points)} / {fmt(r.max_points)}
                    </td>
                  </tr>
                  {!r.correct && (
                    <tr style={{ borderBottom: "1px solid var(--rule)" }}>
                      <td />
                      <td colSpan={2} style={{ padding: "0 0 14px" }}>
                        <div style={{ marginBottom: 6 }}>
                          <Markdown text={r.prompt} />
                        </div>
                        <p style={{ margin: "4px 0" }}>
                          <span className="label">{t("your_answer")}: </span>
                          {givenText(r, me.answers[r.question_id])}
                        </p>
                        <p style={{ margin: "4px 0" }}>
                          <span className="label">{t("correct_answer")}: </span>
                          {correctText(r)}
                        </p>
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </main>
  );
}
