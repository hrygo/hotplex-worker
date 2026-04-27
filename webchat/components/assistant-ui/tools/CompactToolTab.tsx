"use client";

import { motion } from "framer-motion";
import { ToolName } from "@/lib/tool-categories";

interface CompactToolTabProps {
  toolName: string;
  summary: string;
  status: "complete" | "error";
  onClick?: () => void;
}

export function CompactToolTab({ toolName, summary, status, onClick }: CompactToolTabProps) {
  const isError = status === "error";

  return (
    <motion.div
      onClick={onClick}
      className={`
        group flex items-center gap-3 px-4 h-8 mb-2 rounded-xl cursor-pointer
        border transition-all duration-300 backdrop-blur-md
        ${isError 
          ? "bg-[var(--accent-coral)]/5 border-[var(--accent-coral)]/20 hover:bg-[var(--accent-coral)]/10" 
          : "bg-[var(--accent-emerald)]/5 border-[var(--accent-emerald)]/20 hover:bg-[var(--accent-emerald)]/10"}
      `}
    >
      <div className={`
        w-2 h-2 rounded-full shadow-sm
        ${isError ? "bg-[var(--accent-coral)]" : "bg-[var(--accent-emerald)] animate-pulse-subtle"}
      `} />
      
      <span className={`
        text-[11px] font-mono font-bold tracking-tight
        ${isError ? "text-[var(--accent-coral)]" : "text-[var(--accent-emerald)]"}
      `}>
        {toolName.toUpperCase()}
      </span>

      <span className="text-[11px] text-[var(--text-muted)] truncate max-w-[200px]">
        {summary}
      </span>

      <div className="ml-auto flex items-center gap-2">
        <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-tighter">
          {isError ? "Failed" : "Processed"}
        </span>
        <svg 
          className="w-3 h-3 text-[var(--text-faint)] group-hover:text-[var(--text-muted)] transition-colors" 
          fill="none" 
          stroke="currentColor" 
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </div>
    </motion.div>
  );
}
