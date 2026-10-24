import { lazy, Suspense } from "react";
import ReactMarkdown from "react-markdown";
import rehypeSanitize from "rehype-sanitize";
import { components, schema } from "./markdownParts";

// KaTeX (fonts and CSS included) loads only when a text actually has math.
const MathMarkdown = lazy(() => import("./MathMarkdown"));

const hasMath = (text: string) => /\$[^$\n]+\$|\$\$/.test(text);

export interface MarkdownProps {
  text: string;
  inline?: boolean;
}

export function Markdown({ text, inline = false }: MarkdownProps) {
  const plain = (
    <ReactMarkdown rehypePlugins={[[rehypeSanitize, schema]]} components={components(inline)}>
      {text}
    </ReactMarkdown>
  );
  const Wrapper = inline ? "span" : "div";
  if (!hasMath(text)) return <Wrapper className="content">{plain}</Wrapper>;
  return (
    <Wrapper className="content">
      <Suspense fallback={plain}>
        <MathMarkdown text={text} inline={inline} />
      </Suspense>
    </Wrapper>
  );
}
