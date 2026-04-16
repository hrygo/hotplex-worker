"use client";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
// eslint-disable-next-line @typescript-eslint/no-var-requires
const hljs = require("highlight.js");

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

export function MarkdownText({ text }: { text: string }) {
  if (!text) return null;

  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        pre: ({ children }) => <>{children}</>,
        code: ({ className, children, ...props }) => {
          const raw = String(children).replace(/\n$/, "");
          const langMatch = /language-(\w+)/.exec(className ?? "");
          const lang = langMatch?.[1] ?? "";

          if (!className) {
            return (
              <code
                style={{
                  background: "rgba(245, 158, 11, 0.08)",
                  padding: "0.1em 0.3em",
                  borderRadius: "0.25rem",
                  fontSize: "0.875em",
                  fontFamily: "var(--font-mono)",
                  color: "var(--accent-amber)",
                  border: "1px solid rgba(245, 158, 11, 0.1)",
                }}
                {...(props as React.HTMLAttributes<HTMLElement>)}
              >
                {raw}
              </code>
            );
          }

          let highlighted: string;
          if (lang && hljs.getLanguage(lang)) {
            highlighted = hljs.highlight(raw, {
              language: lang,
              ignoreIllegals: true,
            }).value;
          } else {
            highlighted = escapeHtml(raw);
          }

          return (
            <div className="code-block-wrapper">
              <div className="code-block-header">
                <span className="code-lang-label">{lang || "code"}</span>
              </div>
              <code
                style={{
                  display: "block",
                  background: "#16130f",
                  color: "#d6d3d1",
                  padding: "1rem 1.25rem",
                  borderRadius: 0,
                  overflowX: "auto",
                  margin: 0,
                  fontSize: "0.8125em",
                  fontFamily: "var(--font-mono)",
                  lineHeight: 1.75,
                }}
                dangerouslySetInnerHTML={{ __html: highlighted }}
                {...(props as React.HTMLAttributes<HTMLElement>)}
              />
            </div>
          );
        },
        a: ({ href, children }) => (
          <a href={href} target="_blank" rel="noopener noreferrer" style={{ color: "var(--accent-amber)" }}>
            {children}
          </a>
        ),
        table: ({ children }) => (
          <div style={{ overflowX: "auto", margin: "0.5rem 0" }}>
            <table style={{ minWidth: "100%" }}>{children}</table>
          </div>
        ),
      }}
    >
      {text}
    </ReactMarkdown>
  );
};
