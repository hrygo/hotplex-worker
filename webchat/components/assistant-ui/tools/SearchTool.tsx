"use client";

import { motion } from "framer-motion";
import { ToolLoadingSkeleton } from "./ToolLoadingSkeleton";
import { ToolName } from "@/lib/tool-categories";

interface SearchToolProps {
  toolName: string;
  query?: string;
  results?: Array<{ file: string; line?: number; text: string; match?: string }>;
  status: "running" | "complete" | "error";
  onToggle?: () => void;
}

export function SearchTool({ toolName, query, results, status, onToggle }: SearchToolProps) {
  return (
    <div className="rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-default)] my-4 shadow-[0_8px_32px_rgba(0,0,0,0.5)]">
      {/* Header */}
      <div 
        className={`flex items-center gap-2 px-3 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)] ${onToggle ? "cursor-pointer hover:bg-[var(--bg-hover)] transition-colors" : ""}`}
        onClick={onToggle}
      >
        <svg className="w-3.5 h-3.5 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <span className="text-[11px] font-mono text-[var(--text-secondary)]">
          {toolName} {query && <span className="text-[var(--accent-gold)] ml-1">"{query}"</span>}
        </span>
        {onToggle && status !== "running" && (
          <div className="ml-auto text-[var(--text-faint)]">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
            </svg>
          </div>
        )}
        {!onToggle && status === "complete" && results && (
          <span className="ml-auto text-[9px] font-mono text-[var(--text-faint)]">
            {results.length} result{results.length !== 1 ? "s" : ""}
          </span>
        )}
      </div>

      {/* Running skeleton */}
      {status === "running" && (
        <ToolLoadingSkeleton color="var(--accent-violet)" label="Searching..." />
      )}

      {/* Results */}
      {status === "complete" && results && results.length > 0 && (
        <div className="bg-[#0c0c0f] max-h-[250px] overflow-y-auto divide-y divide-[var(--border-subtle)]">
          {results.map((r, i) => (
            <div key={i} className="px-3 py-2 flex items-start gap-2 hover:bg-[var(--bg-hover)] transition-colors">
              <span className="text-[10px] font-mono text-[var(--text-faint)] select-none mt-0.5 shrink-0">
                {r.line ?? i + 1}
              </span>
              <div className="min-w-0 flex-1">
                <span className="text-[10px] font-mono text-[var(--accent-blue)] block truncate">
                  {r.file}
                </span>
                <span className="text-[12px] font-mono text-[var(--text-muted)] block truncate">
                  {r.text}
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      {status === "complete" && results && results.length === 0 && (
        <div className="bg-[#0c0c0f] px-4 py-4 text-center">
          <span className="text-[11px] font-mono text-[var(--text-faint)]">No results found</span>
        </div>
      )}
    </div>
  );
}
