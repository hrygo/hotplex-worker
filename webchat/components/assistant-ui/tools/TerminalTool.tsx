"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { ToolLoadingSkeleton } from "./ToolLoadingSkeleton";

interface TerminalToolProps {
  command: string;
  stdout?: string;
  stderr?: string;
  status: "running" | "complete" | "error";
  onToggle?: () => void;
}

const MAX_VISIBLE_LINES = 15;

export function TerminalTool({ command, stdout, stderr, status, onToggle }: TerminalToolProps) {
  const [expanded, setExpanded] = useState(false);
  const output = stderr || stdout || "";
  const lines = output.split("\n").filter(Boolean);
  const needsCollapse = lines.length > MAX_VISIBLE_LINES && !expanded;
  const displayLines = needsCollapse ? lines.slice(0, MAX_VISIBLE_LINES) : lines;
  const hasError = !!stderr;

  return (
    <div className="rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-default)] my-4 shadow-[0_8px_32px_rgba(0,0,0,0.5)]">
      {/* Terminal header */}
      <div 
        className={`flex items-center gap-2 px-3 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)] ${onToggle ? "cursor-pointer hover:bg-[var(--bg-hover)] transition-colors" : ""}`}
        onClick={onToggle}
      >
        <div className="flex gap-1.5">
          <span className="w-2.5 h-2.5 rounded-full bg-[var(--accent-coral)] opacity-80" />
          <span className="w-2.5 h-2.5 rounded-full bg-[var(--accent-gold)] opacity-80" />
          <span className="w-2.5 h-2.5 rounded-full bg-[var(--accent-emerald)] opacity-80" />
        </div>
        <span className="text-[10px] font-mono font-bold tracking-widest text-[var(--text-faint)] uppercase ml-2">
          Terminal
        </span>
        {status === "running" && (
          <motion.span
            className="ml-auto text-[10px] font-mono text-[var(--accent-emerald)] flex items-center gap-1"
            animate={{ opacity: [0.5, 1, 0.5] }}
            transition={{ repeat: Infinity, duration: 1.5 }}
          >
            <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-emerald)]" />
            Executing...
          </motion.span>
        )}
        {onToggle && status !== "running" && (
          <div className="ml-auto text-[var(--text-faint)]">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
            </svg>
          </div>
        )}
      </div>

      {/* Command */}
      <div className="px-4 py-2.5 bg-[#0c0c0f] border-b border-[var(--border-subtle)]">
        <span className="text-[var(--accent-emerald)] font-mono text-[13px] select-none mr-2">$</span>
        <span className="font-mono text-[13px] text-[var(--text-primary)]">{command}</span>
        {status === "running" && (
          <motion.span
            className="inline-block w-2 h-4 ml-1 bg-[var(--accent-emerald)]"
            animate={{ opacity: [1, 0] }}
            transition={{ repeat: Infinity, duration: 0.8 }}
          />
        )}
      </div>

      {/* Output */}
      {output && (
        <div className="bg-[#0c0c0f] px-4 py-3 max-h-[400px] overflow-y-auto">
          {displayLines.map((line, i) => (
            <div
              key={i}
              className={`font-mono text-[13px] leading-relaxed ${
                hasError ? "text-[var(--accent-coral)]" : "text-[var(--text-muted)]"
              }`}
            >
              {line}
            </div>
          ))}
          {needsCollapse && (
            <button
              onClick={() => setExpanded(true)}
              className="mt-2 text-[11px] font-mono text-[var(--accent-blue)] hover:underline underline-offset-2 transition-colors"
            >
              Expand Output ({lines.length - MAX_VISIBLE_LINES} more lines)
            </button>
          )}
        </div>
      )}

      {/* Running skeleton */}
      {status === "running" && !output && (
        <ToolLoadingSkeleton color="var(--accent-emerald)" label="Waiting for output..." />
      )}
    </div>
  );
}
