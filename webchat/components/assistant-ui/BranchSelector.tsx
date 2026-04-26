"use client";

import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";

interface BranchSelectorProps {
  branches: Array<{ id: string; label: string }>;
  activeBranchId: string;
  onSelect: (branchId: string) => void;
}

export function BranchSelector({ branches, activeBranchId, onSelect }: BranchSelectorProps) {
  const [open, setOpen] = useState(false);

  if (branches.length <= 1) return null;

  const activeBranch = branches.find((b) => b.id === activeBranchId);

  return (
    <div className="relative inline-flex items-center">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 px-2 py-1 rounded-[var(--radius-xs)] text-[10px] font-mono text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all"
      >
        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
        </svg>
        <span>{activeBranch?.label ?? `Branch ${branches.indexOf(activeBranch!) + 1}`}</span>
        <span className="opacity-50">({branches.length})</span>
      </button>

      <AnimatePresence>
        {open && (
          <>
            <div className="fixed inset-0 z-[150]" onClick={() => setOpen(false)} />
            <motion.div
              className="absolute left-0 top-full mt-1 z-[200] min-w-[140px] rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-default)] shadow-[0_12px_32px_rgba(0,0,0,0.5)] overflow-hidden"
              initial={{ opacity: 0, y: -4, scale: 0.95 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: -4, scale: 0.95 }}
            >
              {branches.map((branch, i) => (
                <button
                  key={branch.id}
                  onClick={() => {
                    onSelect(branch.id);
                    setOpen(false);
                  }}
                  className={`w-full px-3 py-2 text-left text-[11px] font-mono flex items-center gap-2 transition-colors ${
                    branch.id === activeBranchId
                      ? "bg-[var(--amber-light)] text-[var(--accent-gold)]"
                      : "text-[var(--text-secondary)] hover:bg-[var(--bg-hover)]"
                  }`}
                >
                  <span className="opacity-50">{i + 1}.</span>
                  {branch.label}
                </button>
              ))}
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}
