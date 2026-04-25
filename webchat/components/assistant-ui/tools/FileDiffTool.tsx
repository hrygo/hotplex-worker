"use client";

import { useState } from "react";
import { motion } from "framer-motion";

interface FileDiffToolProps {
  toolName: string;
  filePath?: string;
  content?: string;
  status: "running" | "complete";
}

export function FileDiffTool({ toolName, filePath, content, status }: FileDiffToolProps) {
  const [copied, setCopied] = useState(false);
  const lines = content?.split("\n") ?? [];

  const handleCopy = () => {
    if (content) {
      navigator.clipboard.writeText(content);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const displayPath = filePath || "unknown file";

  return (
    <div className="rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-default)] my-4 shadow-[0_8px_32px_rgba(0,0,0,0.5)]">
      {/* File header */}
      <div className="flex items-center gap-2 px-3 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)]">
        <svg className="w-3.5 h-3.5 text-[var(--accent-blue)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
        </svg>
        <span className="text-[11px] font-mono text-[var(--text-secondary)] truncate flex-1" title={displayPath}>
          {displayPath}
        </span>
        <span className="text-[9px] font-mono font-bold tracking-widest text-[var(--text-faint)] uppercase">
          {toolName.replace(/_/g, " ")}
        </span>
        {status === "complete" && content && (
          <button
            onClick={handleCopy}
            className="text-[9px] font-mono font-bold tracking-wider text-[var(--text-faint)] hover:text-[var(--accent-gold)] transition-colors flex items-center gap-1 ml-2"
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
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                </svg>
                COPY
              </>
            )}
          </button>
        )}
      </div>

      {/* Code content */}
      {status === "running" && !content && (
        <div className="bg-[#0c0c0f] px-4 py-6 flex items-center gap-3">
          <motion.div
            className="flex gap-1"
            animate={{ opacity: [0.3, 1, 0.3] }}
            transition={{ repeat: Infinity, duration: 1.2 }}
          >
            {[0, 1, 2].map((i) => (
              <span key={i} className="w-1.5 h-1.5 rounded-full bg-[var(--accent-blue)] opacity-60" />
            ))}
          </motion.div>
          <span className="text-[11px] font-mono text-[var(--text-faint)]">Patching...</span>
        </div>
      )}

      {content && (
        <div className="bg-[#0c0c0f] max-h-[300px] overflow-y-auto">
          <table className="w-full border-collapse">
            <tbody>
              {lines.map((line, i) => {
                const lineNum = i + 1;
                const isAdd = line.startsWith("+") && !line.startsWith("+++");
                const isDel = line.startsWith("-") && !line.startsWith("---");
                return (
                  <tr key={i} className={`font-mono text-[13px] leading-relaxed ${
                    isAdd ? "bg-[rgba(52,211,153,0.06)]" :
                    isDel ? "bg-[rgba(244,63,94,0.06)]" : ""
                  }`}>
                    <td className="px-3 py-0 text-right text-[var(--text-faint)] select-none w-[1%] whitespace-nowrap opacity-50">
                      {lineNum}
                    </td>
                    <td className={`px-3 py-0 whitespace-pre ${
                      isAdd ? "text-[var(--accent-emerald)]" :
                      isDel ? "text-[var(--accent-coral)]" :
                      "text-[var(--text-muted)]"
                    }`}>
                      {line}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
