"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { ToolLoadingSkeleton } from "./ToolLoadingSkeleton";
import { useCopyToClipboard } from "@/lib/hooks/useCopyToClipboard";

interface FileDiffToolProps {
  toolName: string;
  filePath?: string;
  content?: string;
  status: "running" | "complete";
}

export function FileDiffTool({ toolName, filePath, content, status }: FileDiffToolProps) {
  const { copied, copy } = useCopyToClipboard();
  const lines = content?.split("\n") ?? [];
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
            onClick={() => content && copy(content)}
            className="text-[9px] font-mono font-bold tracking-wider text-[var(--text-muted)] hover:text-[var(--accent-gold)] transition-colors flex items-center gap-1.5 ml-2 bg-[var(--bg-elevated)] px-2 py-0.5 rounded border border-[var(--border-subtle)]"
          >
            {copied ? (
              <>
                <svg className="w-2.5 h-2.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                </svg>
                COPIED
              </>
            ) : (
              <>
                <svg className="w-2.5 h-2.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 012-2v-8a2 2 0 01-2-2h-8a2 2 0 01-2 2v8a2 2 0 012 2z" />
                </svg>
                COPY
              </>
            )}
          </button>
        )}
      </div>

      {/* Running skeleton */}
      {status === "running" && !content && (
        <ToolLoadingSkeleton color="var(--accent-blue)" label="Patching..." />
      )}

      {/* Code content */}
      {content && (
        <div className="bg-[#0c0c0f] max-h-[300px] overflow-y-auto">
          <table className="w-full border-collapse">
            <tbody>
              {lines.map((line, i) => {
                const isAdd = line.startsWith("+") && !line.startsWith("+++");
                const isDel = line.startsWith("-") && !line.startsWith("---");
                return (
                  <tr key={i} className={`font-mono text-[13px] leading-relaxed ${
                    isAdd ? "bg-[rgba(52,211,153,0.06)]" :
                    isDel ? "bg-[rgba(244,63,94,0.06)]" : ""
                  }`}>
                    <td className="px-3 py-0 text-right text-[var(--text-faint)] select-none w-[1%] whitespace-nowrap opacity-50">
                      {i + 1}
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
