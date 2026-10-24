import { useEffect, useRef, useState } from "react";
import { Check } from "lucide-react";
import type { Response } from "../api/client";
import type { QuestionView } from "../api/types";
import { t } from "../i18n";
import { Markdown } from "./Markdown";
import { iconProps } from "./ui";

export interface AnswerProps {
  question: QuestionView;
  value: Response | undefined;
  disabled: boolean;
  onChange: (r: Response) => void;
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

function Choice({ question, value, disabled, onChange }: AnswerProps) {
  const multi = question.type === "multiple";
  const chosen = new Set(multi ? (value?.options ?? []) : value?.option ? [value.option] : []);
  const toggle = (id: string) => {
    if (disabled) return;
    if (!multi) return onChange(chosen.has(id) ? {} : { option: id });
    const next = new Set(chosen);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    const ordered = (question.options ?? []).map((o) => o.id).filter((o) => next.has(o));
    onChange(ordered.length ? { options: ordered } : {});
  };
  return (
    <div className="options" role={multi ? "group" : "radiogroup"} aria-labelledby="prompt" style={{ marginTop: 24 }}>
      {(question.options ?? []).map((o, i) => {
        const on = chosen.has(o.id);
        return (
          <button
            key={o.id}
            type="button"
            role={multi ? "checkbox" : "radio"}
            aria-checked={on}
            aria-disabled={disabled}
            className={`option ${multi ? "option--multi" : ""} ${on ? "is-selected" : ""} ${disabled ? "is-locked" : ""}`}
            onClick={() => toggle(o.id)}
          >
            <span className="option__key">{KEYS[i]}</span>
            <span className="option__text">
              <Markdown text={o.text} inline />
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
