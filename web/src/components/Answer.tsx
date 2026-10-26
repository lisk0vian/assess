import { useEffect, useMemo, useRef, useState } from "react";
import { Check } from "lucide-react";
import type { Response } from "../api/client";
import type { QuestionView } from "../api/types";
import { t } from "../i18n";
import { Markdown } from "./Markdown";
import { iconProps } from "./ui";
import { fromHex, unseal } from "../proctor/crypto";

export interface AnswerProps {
  question: QuestionView;
  value: Response | undefined;
  disabled: boolean;
  onChange: (r: Response) => void;
  /** Hex attempt key that unseals hover-only options. */
  sealKey?: string;
  /** The hint is rendered on the lens controls line instead. */
  hideHint?: boolean;
}

const KEYS = "ABCDEFGHIJ";

/** The answer area for every question type. */
export function Answer(props: AnswerProps) {
  switch (props.question.type) {
    case "single":
    case "multiple":
      return <Choice {...props} />;
    case "truefalse":
      return <TrueFalse {...props} />;
    default:
      return <Short {...props} />;
  }
}

function Choice({ question, value, disabled, onChange, sealKey, hideHint }: AnswerProps) {
  const multi = question.type === "multiple";
  const chosen = new Set(multi ? (value?.options ?? []) : value?.option ? [value.option] : []);

  // reveal: hover — one option readable at a time, only while pointed at or
  // focused. On touch the first tap reveals and the second one chooses.
  const hover = question.reveal === "hover";
  const key = useMemo(() => (sealKey ? fromHex(sealKey) : null), [sealKey]);
  const [shown, setShown] = useState<string | null>(null);
  const [leaving, setLeaving] = useState<string | null>(null);
  const tapReveal = useRef<string | null>(null);
  const hideTimer = useRef<number | undefined>(undefined);
  const exitTimer = useRef<number | undefined>(undefined);
  useEffect(
    () => () => {
      window.clearTimeout(hideTimer.current);
      window.clearTimeout(exitTimer.current);
    },
    [],
  );
  useEffect(() => {
    setShown(null);
    setLeaving(null);
    window.clearTimeout(hideTimer.current);
    window.clearTimeout(exitTimer.current);
  }, [question.id]);
  const show = (id: string) => {
    window.clearTimeout(hideTimer.current);
    window.clearTimeout(exitTimer.current);
    setLeaving(null);
    setShown(id);
  };
  const hide = (id: string) => {
    window.clearTimeout(exitTimer.current);
    setLeaving(id);
    exitTimer.current = window.setTimeout(() => {
      setShown((s) => (s === id ? null : s));
      setLeaving(null);
    }, 140);
  };
  // A chosen option stays readable so the student can confirm it; the rest
  // remain sealed until pointed at, focused or tapped.
  const textOf = (o: { id: string; text?: string; sealed?: string }, chosenHere: boolean) => {
    if (!hover) return o.text ?? "";
    if ((!chosenHere && shown !== o.id) || !key || !o.sealed) return null;
    return unseal(key, question.id, o.id, o.sealed);
  };
  const touchOnly = typeof matchMedia !== "undefined" && matchMedia("(pointer: coarse)").matches;

  const toggle = (id: string) => {
    if (tapReveal.current === id) {
      tapReveal.current = null;
      return;
    }
    if (disabled) return;
    if (!multi) return onChange(chosen.has(id) ? {} : { option: id });
    const next = new Set(chosen);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    const ordered = (question.options ?? []).map((o) => o.id).filter((o) => next.has(o));
    onChange(ordered.length ? { options: ordered } : {});
  };
  return (
    <>
      {hover && !hideHint && <p className="label reveal-hint">{t(touchOnly ? "reveal_touch" : "reveal_hint")}</p>}
      <div className="options" role={multi ? "group" : "radiogroup"} aria-labelledby="prompt" style={{ marginTop: hover ? 10 : 24 }}>
        {(question.options ?? []).map((o, i) => {
          const on = chosen.has(o.id);
          const text = textOf(o, on);
          return (
            <button
              key={o.id}
              type="button"
              role={multi ? "checkbox" : "radio"}
              aria-checked={on}
              aria-disabled={disabled}
              aria-label={text === null ? t("veiled", { k: KEYS[i] }) : undefined}
              className={`option ${multi ? "option--multi" : ""} ${on ? "is-selected" : ""} ${disabled ? "is-locked" : ""} ${text === null ? "is-veiled" : ""}`}
              onClick={() => toggle(o.id)}
              onPointerEnter={hover ? (e) => e.pointerType !== "touch" && show(o.id) : undefined}
              onPointerLeave={hover ? (e) => e.pointerType !== "touch" && hide(o.id) : undefined}
              onPointerDown={
                hover
                  ? (e) => {
                      if (e.pointerType !== "touch" || shown === o.id) return;
                      tapReveal.current = o.id;
                      show(o.id);
                      hideTimer.current = window.setTimeout(() => hide(o.id), 4000);
                    }
                  : undefined
              }
              onFocus={hover ? () => show(o.id) : undefined}
              onBlur={hover ? () => hide(o.id) : undefined}
            >
              <span className="option__key">{KEYS[i]}</span>
              <span className="option__text">
                {text === null ? (
                  <span className="veil" style={{ width: `${Math.min(34, Math.max(8, Math.round(((o.sealed?.length ?? 0) * 3) / 4)))}ch` }} />
                ) : (
                  <span className={`option__reveal ${leaving === o.id ? "is-out" : "is-in"}`}>
                    <Markdown text={text} inline />
                  </span>
                )}
              </span>
              {on ? (
                <span className="option__mark label">
                  {multi && <Check {...iconProps} size={16} />}
                  {multi ? t("chosen") : t("your_answer")}
                </span>
              ) : (
                <span />
              )}
            </button>
          );
        })}
      </div>
    </>
  );
}

function TrueFalse({ value, disabled, onChange }: AnswerProps) {
  const pick = (v: boolean) => !disabled && onChange(value?.value === v ? {} : { value: v });
  return (
    <div className="tf" role="radiogroup" aria-labelledby="prompt" style={{ marginTop: 24 }}>
      {[true, false].map((v) => (
        <button
          key={String(v)}
          type="button"
          role="radio"
          aria-checked={value?.value === v}
          className={`tf__opt ${value?.value === v ? "is-selected" : ""}`}
          onClick={() => pick(v)}
        >
          <span className="kbd">{v ? "V" : "F"}</span>
          {v ? t("true") : t("false")}
        </button>
      ))}
    </div>
  );
}

function Short({ question, value, disabled, onChange }: AnswerProps) {
  const [text, setText] = useState(value?.text ?? "");
  const timer = useRef<number | undefined>(undefined);
  const numeric = question.input === "number";

  // Another question's value must not leak into this field.
  useEffect(() => setText(value?.text ?? ""), [question.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const change = (v: string) => {
    setText(v);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => onChange(v.trim() ? { text: v } : {}), 350);
  };
  useEffect(() => () => window.clearTimeout(timer.current), []);

  return (
    <label className="field" style={{ marginTop: 24, maxWidth: 520 }}>
      <span className="label">{t("your_answer")}</span>
      <input
        className={`input ${numeric ? "" : "input--text"} ${text.trim() ? "is-answer" : ""}`}
        value={text}
        disabled={disabled}
        inputMode={numeric ? "decimal" : "text"}
        autoComplete="off"
        spellCheck={false}
        placeholder={question.placeholder}
        maxLength={500}
        onChange={(e) => change(e.target.value)}
        onBlur={() => {
          window.clearTimeout(timer.current);
          onChange(text.trim() ? { text } : {});
        }}
      />
      <span className="field__hint">{numeric ? t("answer_number_hint") : t("answer_text_hint")}</span>
    </label>
  );
}
