"use client";

import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useCopyToClipboard } from "@/lib/hooks/useCopyToClipboard";
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
  const { copied, copy } = useCopyToClipboard();
  const [isExpanded, setIsExpanded] = useState(false);
  const lineCount = raw.split("\n").length;
  const isExpandable = lineCount > 10;
  const showContent = !isExpandable || isExpanded;

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    copy(raw);
  };

  return (
    <div className="relative group/code my-6 rounded-[var(--radius-lg)] overflow-hidden border border-[var(--border-default)] bg-[#050507] shadow-2xl transition-all duration-500 hover:border-[var(--border-bright)]">
      <div 
        className={`flex items-center justify-between px-4 py-2.5 bg-[#0c0c0f] border-b border-[var(--border-subtle)] relative overflow-hidden ${isExpandable ? "cursor-pointer hover:bg-[var(--bg-hover)]" : ""}`}
        onClick={() => isExpandable && setIsExpanded(!isExpanded)}
      >
        {/* Subtle Header Shimmer */}
        <div className="absolute inset-0 bg-gradient-to-r from-transparent via-white/[0.02] to-transparent -translate-x-full group-hover/code:animate-shimmer pointer-events-none" />
        
        <div className="flex items-center gap-3 relative z-10">
          <div className="flex items-center gap-1.5 px-2 py-0.5 rounded-md bg-[var(--bg-elevated)] border border-[var(--border-subtle)]">
            <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-gold)] shadow-[0_0_8px_var(--accent-gold)]" />
            <span className="text-[10px] font-mono font-bold tracking-[0.15em] text-[var(--text-primary)] uppercase">
              {lang || "CODE"}
            </span>
          </div>
          {isExpandable && (
            <span className="text-[10px] font-mono text-[var(--text-faint)] tracking-wider">
              {lineCount} LINES {isExpanded ? "· CLICK TO COLLAPSE" : "· CLICK TO EXPAND"}
            </span>
          )}
        </div>
        <div className="flex items-center gap-3 relative z-10">
          <button
            onClick={handleCopy}
            className={`text-[10px] font-mono font-bold tracking-widest px-3 py-1 rounded-md border transition-all active:scale-95 flex items-center gap-2 ${
              copied 
                ? "bg-[rgba(16,185,129,0.1)] border-[var(--accent-emerald)] text-[var(--accent-emerald)]" 
                : "bg-[var(--bg-elevated)] border-[var(--border-subtle)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:border-[var(--border-bright)]"
            }`}
          >
            {copied ? (
              <>
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                </svg>
                COPIED
              </>
            ) : (
              <>
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 012-2v-8a2 2 0 01-2-2h-8a2 2 0 01-2 2v8a2 2 0 012 2z" />
                </svg>
                COPY
              </>
            )}
          </button>
        </div>
      </div>
      <div 
        className="p-4 overflow-x-auto custom-scrollbar transition-[max-height] duration-300 ease-in-out relative"
        style={{ 
          maxHeight: showContent ? "none" : "200px",
          overflowY: showContent ? "auto" : "hidden"
        }}
      >
        <code
          className="block font-mono text-[13px] leading-relaxed whitespace-pre"
          dangerouslySetInnerHTML={{ __html: highlighted }}
        />
        {!showContent && (
          <div 
            className="absolute bottom-0 left-0 right-0 h-12 bg-gradient-to-t from-[#0c0c0f] to-transparent flex items-end justify-center pb-2 cursor-pointer"
            onClick={() => setIsExpanded(true)}
          >
            <span className="text-[10px] font-bold text-[var(--accent-gold)] tracking-widest bg-[var(--bg-surface)] px-2 py-1 rounded border border-[var(--border-subtle)] shadow-lg">
              SHOW MORE
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

export function MarkdownText({ text }: { text: string }) {
  if (!text) return null;

  return (
    <div className="prose prose-invert max-w-none prose-p:first:mt-0 prose-p:last:mb-0">
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

