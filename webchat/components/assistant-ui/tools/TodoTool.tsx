"use client";

import { motion } from "framer-motion";
import { ToolLoadingSkeleton } from "./ToolLoadingSkeleton";

interface Task {
  text: string;
  completed: boolean;
  priority?: "low" | "medium" | "high";
}

interface TodoToolProps {
  todo?: string;
  status: "running" | "complete";
}

export function TodoTool({ todo, status }: TodoToolProps) {
  // Parse markdown-style TODOs if string is provided
  const parseTasks = (text: string): Task[] => {
    return text.split("\n")
      .filter(line => line.trim().startsWith("- ["))
      .map(line => ({
        text: line.replace(/- \[[x ]\] /i, "").trim(),
        completed: /- \[x\]/i.test(line),
      }));
  };

  const tasks = todo ? parseTasks(todo) : [];
  const completedCount = tasks.filter(t => t.completed).length;
  const progress = tasks.length > 0 ? (completedCount / tasks.length) * 100 : 0;

  return (
    <div className="rounded-[var(--radius-lg)] overflow-hidden border border-[var(--border-default)] my-6 bg-[var(--bg-surface)]/50 backdrop-blur-xl shadow-[0_20px_50px_rgba(0,0,0,0.3)]">
      {/* Header with Progress Bar */}
      <div className="px-5 py-4 border-b border-[var(--border-subtle)] bg-gradient-to-r from-[var(--bg-elevated)] to-transparent">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg bg-[var(--accent-violet)]/10 flex items-center justify-center text-[var(--accent-violet)] shadow-[0_0_15px_rgba(139,92,246,0.2)]">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
              </svg>
            </div>
            <div>
              <h3 className="text-[13px] font-bold text-[var(--text-primary)] tracking-tight">Mission Checklist</h3>
              <p className="text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-wider">Operational Status Update</p>
            </div>
          </div>
          <div className="text-right">
            <span className="text-[18px] font-mono font-bold text-[var(--accent-violet)]">{Math.round(progress)}%</span>
            <p className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-tighter">Completion</p>
          </div>
        </div>
        
        {/* Animated Progress Track */}
        <div className="h-1.5 w-full bg-black/40 rounded-full overflow-hidden relative border border-white/[0.05]">
          <motion.div 
            className="absolute inset-y-0 left-0 bg-gradient-to-r from-[var(--accent-violet)] via-[var(--accent-blue)] to-[var(--accent-emerald)] shadow-[0_0_15px_var(--accent-violet)]"
            initial={{ width: 0 }}
            animate={{ width: `${progress}%` }}
            transition={{ duration: 1, ease: "circOut" }}
          />
          {status === "running" && (
            <motion.div 
              className="absolute inset-0 bg-white/20"
              animate={{ x: ["-100%", "100%"] }}
              transition={{ repeat: Infinity, duration: 1.5, ease: "linear" }}
            />
          )}
        </div>
      </div>

      {/* Task List */}
      <div className="p-2 max-h-[300px] overflow-y-auto">
        {status === "running" && tasks.length === 0 ? (
          <ToolLoadingSkeleton color="var(--accent-violet)" label="Retrieving latest tasks..." />
        ) : (
          <div className="grid grid-cols-1 gap-1">
            {tasks.map((task, i) => (
              <motion.div 
                key={i}
                initial={{ opacity: 0, x: -5 }}
                animate={{ opacity: 1, x: 0 }}
                transition={{ delay: i * 0.05 }}
                className={`flex items-center gap-4 px-4 py-3 rounded-xl transition-all duration-300 ${
                  task.completed ? "bg-white/[0.02] opacity-50" : "hover:bg-white/[0.05] group"
                }`}
              >
                <div className={`w-5 h-5 rounded-md border flex items-center justify-center transition-all duration-500 ${
                  task.completed 
                    ? "bg-[var(--accent-emerald)] border-[var(--accent-emerald)] shadow-[0_0_10px_var(--accent-emerald)]" 
                    : "border-white/20 group-hover:border-[var(--accent-violet)]/50"
                }`}>
                  {task.completed && (
                    <svg className="w-3.5 h-3.5 text-black" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                    </svg>
                  )}
                </div>
                <span className={`text-[13px] font-medium leading-none ${
                  task.completed ? "text-[var(--text-faint)] line-through" : "text-[var(--text-secondary)]"
                }`}>
                  {task.text}
                </span>
                {!task.completed && (
                  <div className="ml-auto w-1.5 h-1.5 rounded-full bg-[var(--accent-violet)] opacity-0 group-hover:opacity-100 transition-opacity animate-pulse" />
                )}
              </motion.div>
            ))}
          </div>
        )}
        
        {status === "complete" && tasks.length === 0 && (
          <div className="py-8 text-center">
             <div className="w-12 h-12 rounded-full bg-white/[0.03] flex items-center justify-center mx-auto mb-3">
                <svg className="w-6 h-6 text-[var(--text-faint)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
             </div>
             <p className="text-[11px] font-mono text-[var(--text-faint)] uppercase tracking-widest">No active tasks recorded</p>
          </div>
        )}
      </div>

      {/* Footer / Meta */}
      <div className="px-5 py-3 bg-black/20 border-t border-white/[0.03] flex items-center justify-between">
        <div className="flex gap-1">
           {[...Array(3)].map((_, i) => (
             <div key={i} className={`w-1 h-1 rounded-full ${i < completedCount ? 'bg-[var(--accent-emerald)]' : 'bg-white/10'}`} />
           ))}
        </div>
        <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-widest">
           {status === "running" ? "Updating Neural Buffer..." : "Sync Complete"}
        </span>
      </div>
    </div>
  );
}
