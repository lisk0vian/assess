import type { ReactNode } from "react";
import type { Components } from "react-markdown";
import { defaultSchema } from "rehype-sanitize";
import { assetURL } from "../api/client";

/** Sanitizer schema: GitHub-like defaults plus the classes math needs. */
export const schema = {
  ...defaultSchema,
  attributes: {
    ...defaultSchema.attributes,
    code: [...(defaultSchema.attributes?.code ?? []), ["className", /^language-./, "math-inline", "math-display"]],
  },
};

function language(children: ReactNode): string {
  const child = Array.isArray(children) ? children[0] : children;
  const cls = (child as { props?: { className?: string } })?.props?.className ?? "";
  return /language-(\w+)/.exec(cls)?.[1] ?? "";
}

/** Element mapping shared by the plain and math renderers. */
export function components(inline: boolean): Components {
  return {
    p: ({ children }) => (inline ? <span>{children}</span> : <p>{children}</p>),
    img: ({ src, alt }) => (
      <img src={assetURL(String(src ?? ""))} alt={alt ?? ""} loading="lazy" decoding="async" draggable={false} />
    ),
    pre: ({ children }) => {
      const lang = language(children);
      return (
        <figure className="code">
          {lang && (
            <figcaption className="code__head">
              <span className="label">{lang}</span>
            </figcaption>
          )}
          <pre>{children}</pre>
        </figure>
      );
    },
    a: ({ children }) => <span>{children}</span>, // links would let students leave the exam
  };
}
