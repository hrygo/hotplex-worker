"use client";

import { motion } from "framer-motion";
import { ToolLoadingSkeleton } from "./ToolLoadingSkeleton";
import { useCopyToClipboard } from "@/lib/hooks/useCopyToClipboard";

interface FileDiffToolProps {
  toolName: string;
  filePath?: string;
  content?: string;
  status: "running" | "complete" | "error";
  onToggle?: () => void;
}

export function FileDiffTool({ toolName, filePath, content, status, onToggle }: FileDiffToolProps) {
  const { copied, copy } = useCopyToClipboard();
  const lines = content?.split("\n") ?? [];
  const displayPath = filePath || "unknown file";

  return (
    <div className="rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-subtle)] my-6 shadow-[0_12px_40px_rgba(0,0,0,0.4)] group/diff">
      {/* File header */}
      <div
        className={`flex items-center gap-3 px-4 py-2.5 bg-[var(--bg-elevated)] border-b border-[var(--border-subtle)] ${onToggle ? "cursor-pointer hover:bg-[var(--bg-hover)] transition-all" : ""}`}
        onClick={onToggle}
      >
        <svg className="w-4 h-4 text-[var(--accent-blue)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
        </svg>
        <span className="text-[11px] font-display font-black tracking-[0.1em] text-[var(--text-faint)] uppercase">
          {toolName.replace(/_/g, " ")}
        </span>
        {displayPath && (
          <span className="text-[12px] text-[var(--text-primary)] font-mono tracking-tight bg-[var(--bg-base)] px-2 py-0.5 rounded-[var(--radius-xs)] border border-[var(--border-subtle)]">
            {displayPath}
          </span>
        )}

        <div className="ml-auto flex items-center gap-3">
          {status === "running" && (
            <motion.span
              className="text-[10px] font-mono text-[var(--accent-blue)] font-bold flex items-center gap-2"
              animate={{ opacity: [0.5, 1, 0.5] }}
              transition={{ repeat: Infinity, duration: 1.5 }}
            >
              <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-blue)] shadow-[0_0_8px_var(--accent-blue)]" />
              PATCHING
            </motion.span>
          )}
          {onToggle && status !== "running" && (
            <div className="text-[var(--text-faint)] transform group-hover/diff:scale-110 transition-transform">
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 15l7-7 7 7" />
              </svg>
            </div>
          )}
          {status === "complete" && content && (
            <button
              onClick={(e) => { e.stopPropagation(); copy(content); }}
              className="p-1.5 rounded-[var(--radius-xs)] hover:bg-[var(--bg-hover)] text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-all transform hover:scale-110 active:scale-95"
              title="Copy content"
            >
              {copied ? (
                <svg className="w-3.5 h-3.5 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                </svg>
              ) : (
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                </svg>
              )}
            </button>
          )}
        </div>
      </div>

      {/* Running skeleton */}
      {status === "running" && !content && (
        <div className="bg-[#08080a] p-8">
          <ToolLoadingSkeleton color="var(--accent-blue)" label="Synthesizing file patch..." />
        </div>
      )}

      {/* Code content */}
      {content && (
        <div className="bg-[#08080a] max-h-[400px] overflow-y-auto scrollbar-thin">
          <table className="w-full border-collapse">
            <tbody>
              {lines.map((line, i) => {
                const isAdd = line.startsWith("+") && !line.startsWith("+++");
                const isDel = line.startsWith("-") && !line.startsWith("---");
                return (
                  <tr key={i} className={`font-mono text-[13px] leading-relaxed transition-colors ${isAdd ? "bg-[var(--accent-emerald)]/[0.08] hover:bg-[var(--accent-emerald)]/[0.12]" :
                      isDel ? "bg-[var(--accent-coral)]/[0.08] hover:bg-[var(--accent-coral)]/[0.12]" :
                        "hover:bg-white/[0.02]"
                    }`}>
                    <td className="px-4 py-0.5 text-right text-[var(--text-faint)] select-none w-[1%] whitespace-nowrap opacity-30 font-mono text-[11px]">
                      {i + 1}
                    </td>
                    <td className={`px-4 py-0.5 whitespace-pre break-all ${isAdd ? "text-[var(--accent-emerald)]" :
                        isDel ? "text-[var(--accent-coral)] font-medium" :
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
