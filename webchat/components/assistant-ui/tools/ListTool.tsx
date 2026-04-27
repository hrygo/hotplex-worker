"use client";

import { motion } from "framer-motion";
import { ToolLoadingSkeleton } from "./ToolLoadingSkeleton";

interface ListItem {
  name: string;
  type: "file" | "directory";
  size?: number;
  children?: number;
}

interface ListToolProps {
  toolName: string;
  path?: string;
  items?: ListItem[];
  status: "running" | "complete";
}

export function ListTool({ toolName, path, items, status }: ListToolProps) {
  return (
    <div className="rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-default)] my-4 shadow-[0_8px_32px_rgba(0,0,0,0.5)]">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)]">
        <svg className="w-3.5 h-3.5 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
        </svg>
        <span className="text-[11px] font-mono text-[var(--text-secondary)]">
          {toolName} {path && <span className="text-[var(--accent-gold)] ml-1">{path}</span>}
        </span>
        {status === "complete" && items && (
          <span className="ml-auto text-[9px] font-mono text-[var(--text-faint)]">
            {items.length} item{items.length !== 1 ? "s" : ""}
          </span>
        )}
      </div>

      {/* Running skeleton */}
      {status === "running" && (
        <ToolLoadingSkeleton color="var(--accent-gold)" label="Listing directory..." />
      )}

      {/* Items */}
      {status === "complete" && items && items.length > 0 && (
        <div className="bg-[#0c0c0f] max-h-[300px] overflow-y-auto">
          <div className="grid grid-cols-[1fr_auto] gap-x-4 px-3 py-2 border-b border-white/[0.03] text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-wider">
            <span>Name</span>
            <span>Info</span>
          </div>
          <div className="divide-y divide-white/[0.03]">
            {items.map((item, i) => (
              <div key={i} className="px-3 py-2 flex items-center justify-between hover:bg-[var(--bg-hover)] transition-colors group">
                <div className="flex items-center gap-2 min-w-0">
                  {item.type === "directory" ? (
                    <svg className="w-3.5 h-3.5 text-[var(--accent-gold)] shrink-0" fill="currentColor" viewBox="0 0 20 20">
                      <path d="M2 6a2 2 0 012-2h5l2 2h5a2 2 0 012 2v6a2 2 0 01-2 2H4a2 2 0 01-2-2V6z" />
                    </svg>
                  ) : (
                    <svg className="w-3.5 h-3.5 text-[var(--text-faint)] shrink-0 group-hover:text-[var(--text-primary)] transition-colors" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z" />
                    </svg>
                  )}
                  <span className={`text-[12px] font-mono truncate ${item.type === 'directory' ? 'text-[var(--text-primary)] font-bold' : 'text-[var(--text-secondary)]'}`}>
                    {item.name}
                  </span>
                </div>
                <div className="text-[10px] font-mono text-[var(--text-faint)] shrink-0">
                  {item.type === "directory" 
                    ? `${item.children ?? 0} items` 
                    : item.size 
                      ? `${(item.size / 1024).toFixed(1)} KB` 
                      : "-"}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {status === "complete" && items && items.length === 0 && (
        <div className="bg-[#0c0c0f] px-4 py-4 text-center">
          <span className="text-[11px] font-mono text-[var(--text-faint)]">Empty directory</span>
        </div>
      )}
    </div>
  );
}
