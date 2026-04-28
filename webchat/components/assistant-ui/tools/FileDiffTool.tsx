"use client";

import { useState } from "react";
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
    <div className="rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-default)] my-4 shadow-[0_8px_32px_rgba(0,0,0,0.5)]">
      {/* File header */}
      <div 
        className={`flex items-center gap-3 px-3 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)] ${onToggle ? "cursor-pointer hover:bg-[var(--bg-hover)] transition-colors" : ""}`}
        onClick={onToggle}
      >
        <svg className="w-4 h-4 text-[var(--accent-blue)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
        </svg>
        <span className="text-[10px] font-mono font-bold tracking-widest text-[var(--text-faint)] uppercase">
          {toolName.replace(/_/g, " ")} {displayPath && <span className="text-[var(--text-primary)] normal-case ml-1 font-mono tracking-tight">{displayPath}</span>}
        </span>
        
        <div className="ml-auto flex items-center gap-2">
          {status === "running" && (
             <motion.span
               className="text-[10px] font-mono text-[var(--accent-blue)] flex items-center gap-1 mr-2"
               animate={{ opacity: [0.5, 1, 0.5] }}
               transition={{ repeat: Infinity, duration: 1.5 }}
             >
               Writing...
             </motion.span>
          )}
          {onToggle && status !== "running" && (
            <div className="text-[var(--text-faint)]">
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
              </svg>
            </div>
          )}
          {status === "complete" && content && (
            <button
              onClick={(e) => { e.stopPropagation(); copy(content); }}
              className="p-1 rounded hover:bg-[var(--bg-hover)] text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors ml-1"
              title="Copy content"
            >
              {copied ? (
                <svg className="w-3.5 h-3.5 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
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
