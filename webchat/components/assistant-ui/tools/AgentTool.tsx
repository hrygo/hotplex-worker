"use client";

import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { MarkdownText } from "../MarkdownText";

interface AgentToolProps {
  description: string;
  prompt: string;
  subagent_type?: string;
  run_in_background?: boolean;
  status: "running" | "complete" | "error";
}

export function AgentTool({ description, prompt, subagent_type, run_in_background, status }: AgentToolProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <div className="rounded-[var(--radius-lg)] overflow-hidden border border-[var(--border-default)] my-6 bg-[var(--bg-surface)]/40 backdrop-blur-md shadow-[0_8px_32px_rgba(0,0,0,0.4)] transition-all duration-500 hover:shadow-[0_12px_48px_rgba(0,0,0,0.5)]">
      {/* Header */}
      <div className="px-5 py-4 bg-gradient-to-r from-[var(--bg-elevated)] to-transparent border-b border-white/[0.05] flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="relative">
            <div className="w-10 h-10 rounded-xl bg-[var(--accent-gold)]/10 flex items-center justify-center text-[var(--accent-gold)] shadow-[0_0_20px_rgba(245,158,11,0.15)] border border-[var(--accent-gold)]/20">
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
              </svg>
            </div>
            {status === "running" && (
              <span className="absolute -top-1 -right-1 flex h-3 w-3">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-[var(--accent-gold)] opacity-75"></span>
                <span className="relative inline-flex rounded-full h-3 w-3 bg-[var(--accent-gold)]"></span>
              </span>
            )}
          </div>
          <div>
            <h3 className="text-[14px] font-bold text-[var(--text-primary)] tracking-tight">Deploying Subagent</h3>
            <div className="flex items-center gap-2 mt-0.5">
              <span className="text-[10px] font-mono text-[var(--accent-gold)] uppercase tracking-wider bg-[var(--accent-gold)]/10 px-1.5 py-0.5 rounded border border-[var(--accent-gold)]/20 font-bold">
                {subagent_type || "autonomous"}
              </span>
              {run_in_background && (
                <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-tighter">
                  • Background Process
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-3">
           <span className={`text-[10px] font-bold uppercase tracking-widest ${status === 'running' ? 'text-[var(--accent-gold)] animate-pulse' : 'text-[var(--text-faint)]'}`}>
             {status === 'running' ? 'Initializing...' : 'Deployed'}
           </span>
        </div>
      </div>

      {/* Main Content */}
      <div className="p-5">
        <div className="flex items-start gap-3 mb-4">
          <div className="mt-1 text-[var(--accent-gold)]">
            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
              <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
            </svg>
          </div>
          <p className="text-[13px] text-[var(--text-secondary)] leading-relaxed font-medium">
            {description}
          </p>
        </div>

        {/* Prompt Section */}
        <div className="rounded-xl bg-black/40 border border-white/[0.05] overflow-hidden">
          <button 
            onClick={() => setIsExpanded(!isExpanded)}
            className="w-full px-4 py-3 flex items-center justify-between hover:bg-white/[0.02] transition-colors group"
          >
            <span className="text-[11px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest group-hover:text-[var(--text-secondary)] transition-colors">
              Operational Instructions
            </span>
            <motion.div
              animate={{ rotate: isExpanded ? 180 : 0 }}
              className="text-[var(--text-faint)]"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M19 9l-7 7-7-7" />
              </svg>
            </motion.div>
          </button>
          
          <AnimatePresence>
            {isExpanded && (
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={{ height: "auto", opacity: 1 }}
                exit={{ height: 0, opacity: 0 }}
                transition={{ duration: 0.3, ease: "easeInOut" }}
              >
                <div className="px-4 pb-4 pt-1">
                  <div className="p-4 rounded-lg bg-white/[0.02] border border-white/[0.03] text-[12px] text-[var(--text-muted)] font-mono leading-relaxed max-h-[400px] overflow-y-auto custom-scrollbar whitespace-pre-wrap">
                    <MarkdownText text={prompt} />
                  </div>
                </div>
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      </div>

      {/* Footer / Status Bar */}
      <div className="px-5 py-3 bg-black/40 border-t border-white/[0.03] flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <div className="flex gap-1">
             {[...Array(3)].map((_, i) => (
               <motion.div 
                 key={i} 
                 className="w-1.5 h-1.5 rounded-full bg-[var(--accent-gold)]"
                 animate={status === 'running' ? { opacity: [0.2, 1, 0.2] } : { opacity: 1 }}
                 transition={{ repeat: Infinity, duration: 1.5, delay: i * 0.2 }}
               />
             ))}
          </div>
          <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-[0.2em] ml-2">
            AI Core Link Active
          </span>
        </div>
        <div className="flex items-center gap-2">
           <div className="h-1 w-24 bg-white/5 rounded-full overflow-hidden">
              <motion.div 
                className="h-full bg-[var(--accent-gold)] shadow-[0_0_8px_var(--accent-gold)]"
                initial={{ width: 0 }}
                animate={{ width: status === 'complete' ? '100%' : '30%' }}
                transition={{ duration: 2 }}
              />
           </div>
        </div>
      </div>
    </div>
  );
}
