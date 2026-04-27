"use client";

import { motion } from "framer-motion";
import { ToolName } from "@/lib/tool-categories";

interface PermissionCardProps {
  toolName: string;
  args?: Record<string, any>;
  status: "running" | "complete";
  onRespond?: (approved: boolean) => void;
  onToggle?: () => void;
}

export function PermissionCard({ toolName, args, status, onRespond, onToggle }: PermissionCardProps) {
  const isElicitation = toolName === ToolName.Elicitation;
  const isPermission = toolName === ToolName.AskPermission || toolName === ToolName.Confirm;
  const title = isElicitation
    ? "Action Required"
    : isPermission
    ? "Permission Request"
    : "Confirmation";

  const description = args?.message || args?.prompt || args?.description || "";
  const command = args?.command || args?.tool || "";

  return (
    <motion.div
      className="rounded-[var(--radius-md)] overflow-hidden border border-[rgba(251,191,36,0.2)] my-4 shadow-[0_8px_32px_rgba(0,0,0,0.5)]"
      initial={{ opacity: 0, scale: 0.98 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ type: "spring" as const, stiffness: 260, damping: 20 }}
    >
      {/* Header */}
      <div 
        className={`flex items-center gap-2 px-4 py-3 bg-[rgba(251,191,36,0.06)] border-b border-[rgba(251,191,36,0.12)] ${onToggle ? "cursor-pointer hover:bg-[rgba(251,191,36,0.1)] transition-colors" : ""}`}
        onClick={onToggle}
      >
        <div className="w-7 h-7 rounded-[var(--radius-sm)] bg-[rgba(251,191,36,0.1)] flex items-center justify-center">
          <svg className="w-4 h-4 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <span className="text-[11px] font-display font-bold text-[var(--accent-gold)] uppercase tracking-wider">
          {title}
        </span>
        {onToggle && status === "complete" && (
          <div className="ml-auto text-[var(--text-faint)]">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
            </svg>
          </div>
        )}
      </div>

      {/* Body */}
      <div className="px-4 py-3 bg-[var(--bg-surface)]">
        {command && (
          <div className="mb-2 px-3 py-1.5 rounded-[var(--radius-sm)] bg-[#0c0c0f] border border-[var(--border-subtle)]">
            <span className="font-mono text-[12px] text-[var(--accent-emerald)] select-none mr-1.5">$</span>
            <span className="font-mono text-[12px] text-[var(--text-primary)]">{command}</span>
          </div>
        )}
        {description && (
          <p className="text-sm text-[var(--text-secondary)] leading-relaxed">{description}</p>
        )}
      </div>

      {/* Actions — only show when awaiting response */}
      {status === "running" && (
        <div className="flex items-center gap-2 px-4 py-3 bg-[var(--bg-surface)] border-t border-[var(--border-subtle)]">
          <button onClick={() => onRespond?.(true)} className="flex-1 py-2 rounded-[var(--radius-sm)] bg-[var(--accent-emerald)] text-black font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]">
            Approve
          </button>
          <button onClick={() => onRespond?.(false)} className="flex-1 py-2 rounded-[var(--radius-sm)] bg-[var(--accent-coral)] text-white font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]">
            Reject
          </button>
        </div>
      )}

      {status === "complete" && (
        <div className="flex items-center gap-2 px-4 py-2 bg-[var(--bg-surface)] border-t border-[var(--border-subtle)]">
          <svg className="w-3.5 h-3.5 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
          </svg>
          <span className="text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-wider">Responded</span>
        </div>
      )}
    </motion.div>
  );
}
