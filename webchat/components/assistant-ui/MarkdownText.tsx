"use client";

import { useState } from "react";
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

function CodeBlock({ raw, lang, highlighted }: { raw: string; lang: string; highlighted: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(raw);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="relative group/code my-6 rounded-[var(--radius-lg)] overflow-hidden border border-[var(--border-default)] bg-black shadow-2xl">
      <div className="flex items-center justify-between px-4 py-2 bg-[var(--bg-elevated)] border-bottom border-[var(--border-subtle)]">
        <span className="text-[10px] font-mono font-bold tracking-widest text-[var(--text-muted)] uppercase">
          {lang || "code"}
        </span>
        <button
          onClick={handleCopy}
          className="text-[10px] font-mono font-bold tracking-wider text-[var(--text-muted)] hover:text-[var(--accent-gold)] transition-colors flex items-center gap-1.5"
        >
          {copied ? (
            <>
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
              </svg>
              COPIED
            </>
          ) : (
            <>
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 012-2v-8a2 2 0 01-2-2h-8a2 2 0 01-2 2v8a2 2 0 012 2z" />
              </svg>
              COPY
            </>
          )}
        </button>
      </div>
      <div className="p-4 overflow-x-auto custom-scrollbar">
        <code
          className="block font-mono text-[13px] leading-relaxed"
          dangerouslySetInnerHTML={{ __html: highlighted }}
        />
      </div>
    </div>
  );
}

export function MarkdownText({ text }: { text: string }) {
  if (!text) return null;

  return (
    <div className="prose prose-invert max-w-none">
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
                <code className="bg-[var(--bg-elevated)] text-[var(--accent-gold-bright)] px-1.5 py-0.5 rounded-[var(--radius-xs)] text-[0.9em] font-mono border border-[var(--border-subtle)]">
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

            return <CodeBlock raw={raw} lang={lang} highlighted={highlighted} />;
          },
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noopener noreferrer" className="text-[var(--accent-gold)] hover:underline underline-offset-4 decoration-[var(--accent-gold)]">
              {children}
            </a>
          ),
          table: ({ children }) => (
            <div className="my-6 overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--border-default)]">
              <table className="min-w-full divide-y divide-[var(--border-subtle)] bg-[var(--bg-surface)]">
                {children}
              </table>
            </div>
          ),
          th: ({ children }) => (
            <th className="px-4 py-3 text-left text-xs font-bold uppercase tracking-wider text-[var(--text-muted)] bg-[var(--bg-elevated)]">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="px-4 py-3 text-sm text-[var(--text-secondary)] border-t border-[var(--border-subtle)]">
              {children}
            </td>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

