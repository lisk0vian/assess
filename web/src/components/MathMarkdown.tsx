import ReactMarkdown from "react-markdown";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import rehypeSanitize from "rehype-sanitize";
import "katex/dist/katex.min.css";
import { components, schema } from "./markdownParts";

// Sanitize first, then let KaTeX render its own trusted markup.
export default function MathMarkdown({ text, inline }: { text: string; inline: boolean }) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkMath]}
      rehypePlugins={[[rehypeSanitize, schema], [rehypeKatex, { throwOnError: false, output: "htmlAndMathml" }]]}
      components={components(inline)}
    >
      {text}
    </ReactMarkdown>
  );
}
