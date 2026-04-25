"use client";

import React, { useState } from "react";
import {
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ActionBarPrimitive,
  useMessage,
} from "@assistant-ui/react";
import { motion, AnimatePresence } from "framer-motion";
import { MarkdownText } from "./MarkdownText";
import { TerminalTool } from "./tools/TerminalTool";
import { FileDiffTool } from "./tools/FileDiffTool";
import { SearchTool } from "./tools/SearchTool";
import { PermissionCard } from "./tools/PermissionCard";
import { BrandIcon } from "@/components/icons";

/* ============================================================
   Animation variants — spec §4.4
   ============================================================ */
const messageVariants = {
  hidden: { opacity: 0, y: 10 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { type: "spring" as const, stiffness: 300, damping: 24 },
  },
};

const toolCardVariants = {
  hidden: { opacity: 0, y: 8, scale: 0.98 },
  visible: {
    opacity: 1,
    y: 0,
    scale: 1,
    transition: { type: "spring" as const, stiffness: 260, damping: 20 },
  },
};

/* ============================================================
   Tool Name Router — Maps tool names to specialized GenUI
   ============================================================ */
const TERMINAL_TOOLS = new Set(["run_command", "bash", "execute_command", "shell"]);
const FILE_TOOLS = new Set(["edit_file", "write_file", "replace_file_content", "create_file", "apply_diff"]);
const SEARCH_TOOLS = new Set(["grep_search", "view_file", "search_files", "list_directory", "read_file"]);
const PERMISSION_TOOLS = new Set(["ask_permission", "confirm", "elicitation"]);

function getToolCategory(name: string): "terminal" | "file" | "search" | "permission" | "default" {
  if (TERMINAL_TOOLS.has(name)) return "terminal";
  if (FILE_TOOLS.has(name)) return "file";
  if (SEARCH_TOOLS.has(name)) return "search";
  if (PERMISSION_TOOLS.has(name)) return "permission";
  return "default";
}

/* ============================================================
   Thread — Main conversation wrapper
   ============================================================ */
export function Thread() {
  return (
    <ThreadPrimitive.Root className="flex flex-col h-full relative overflow-hidden">
      {/* Background elements */}
      <div className="bg-mesh" aria-hidden="true" />
      <div className="noise-overlay" aria-hidden="true" />

      {/* Viewport */}
      <ThreadPrimitive.Viewport className="thread-viewport relative" style={{ zIndex: 1 }}>
        <div className="thread-content">
          {/* Welcome */}
          <ThreadPrimitive.Empty>
            <WelcomeScreen />
          </ThreadPrimitive.Empty>

          {/* Messages */}
          <ThreadPrimitive.Messages>
            {({ message }) =>
              message.role === "user" ? <UserMessage /> : <AssistantMessage />
            }
          </ThreadPrimitive.Messages>
        </div>

        {/* Scroll to bottom button */}
        <ThreadPrimitive.ScrollToBottom asChild>
          <button className="scroll-bottom-btn">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M19 14l-7 7-7-7" />
            </svg>
            <span>Jump to Latest</span>
          </button>
        </ThreadPrimitive.ScrollToBottom>
      </ThreadPrimitive.Viewport>

      {/* Composer wrapper (floating footer) */}
      <div className="composer-wrapper">
        <div className="composer-container">
          <Composer />
          <div className="mt-2 text-center">
            <p className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest">
              Shift + Enter for new line · Cmd + Enter to send
            </p>
          </div>
        </div>
      </div>
    </ThreadPrimitive.Root>
  );
}

/* ============================================================
   Welcome Screen
   ============================================================ */
const SUGGESTIONS = [
  { prompt: "帮我写一个 React 组件", icon: "code" },
  { prompt: "解释这段代码的逻辑", icon: "learn" },
  { prompt: "帮我调试这个错误", icon: "debug" },
  { prompt: "重构这段代码让它更简洁", icon: "refactor" },
] as const;

type SuggestionIcon = (typeof SUGGESTIONS)[number]["icon"];

const ICON_PATHS: Record<SuggestionIcon, string> = {
  code: "M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4",
  learn: "M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253",
  debug: "M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z",
  refactor: "M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15",
};

function WelcomeScreen() {
  return (
    <motion.div
      className="flex flex-col items-center justify-center py-24 text-center"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: 0.6, ease: [0.2, 0.8, 0.2, 1] }}
    >
      <div className="relative mb-8 flex items-center justify-center">
        <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-10 blur-3xl rounded-[var(--radius-full)] scale-[2]" />

        {/* Orbital rings */}
        <div className="absolute w-32 h-32 border border-[var(--accent-gold)] opacity-15 rounded-[var(--radius-full)] animate-[spin_10s_linear_infinite]" />
        <div className="absolute w-40 h-40 border border-[var(--accent-emerald)] opacity-8 rounded-[var(--radius-full)] animate-[spin_15s_linear_infinite_reverse]" />

        {/* Orbiting particles */}
        <div className="absolute w-full h-full animate-[orbit_4s_linear_infinite]">
           <div className="w-2 h-2 rounded-[var(--radius-full)] bg-[var(--accent-gold)] blur-[1px]" />
        </div>

        <BrandIcon size={96} className="relative z-10 animate-float" />
      </div>

      <h1 className="text-4xl font-display font-bold tracking-tight mb-3 text-gradient-gold">
        How can I help you today?
      </h1>
      <p className="text-lg text-[var(--text-muted)] mb-12 max-w-lg mx-auto">
        Ask me anything about code, debugging, or software architecture.
        Empowered by HotPlex Intelligence.
      </p>

      <div className="grid grid-cols-2 gap-3 w-full max-w-xl mx-auto">
        {SUGGESTIONS.map((s, i) => {
          const selfContained = s.icon === "code";
          return (
            <ThreadPrimitive.Suggestion
              key={s.prompt}
              prompt={s.prompt}
              {...(selfContained ? { send: true } : {})}
              className="suggestion-card flex items-center gap-3"
            >
              <motion.div
                className="w-8 h-8 rounded-[var(--radius-sm)] bg-[var(--bg-elevated)] flex items-center justify-center text-[var(--accent-gold)]"
                initial={{ opacity: 0, scale: 0.8 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={{ delay: 0.1 + i * 0.05, duration: 0.3 }}
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={ICON_PATHS[s.icon]} />
                </svg>
              </motion.div>
              <span className="font-medium">{s.prompt}</span>
            </ThreadPrimitive.Suggestion>
          );
        })}
      </div>
    </motion.div>
  );
}

/* ============================================================
   Assistant Message
   ============================================================ */
function AssistantMessage() {
  const message = useMessage();

  return (
    <motion.div
      className="group msg-assistant"
      variants={messageVariants}
      initial="hidden"
      animate="visible"
    >
      <div className="flex-shrink-0 mt-1">
        <div className="w-9 h-9 rounded-[var(--radius-md)] glass-dark flex items-center justify-center">
          <BrandIcon size={28} />
        </div>
      </div>

      <div className="msg-assistant-body">
        <MessagePrimitive.Parts>
          {({ part }) => {
            const p = part as any;
            if (!p) return null;
            const m = message as any;
            const isStreaming = m.status?.type === "running";

            if (p.type === "reasoning") {
              return <ReasoningBlock text={p.text} />;
            }
            if (p.type === "text") {
              return (
                <span className={isStreaming ? "streaming-cursor" : ""}>
                  <MarkdownText text={p.text} />
                </span>
              );
            }
            if (p.type === "tool-call") {
              const category = getToolCategory(p.toolName);
              const hasResult = p.result !== undefined;

              // Route to specialized GenUI components
              if (category === "terminal") {
                return (
                  <motion.div key={p.toolCallId} variants={toolCardVariants} initial="hidden" animate="visible">
                    <TerminalTool
                      command={p.args?.command || p.args?.Command || JSON.stringify(p.args)}
                      stdout={hasResult ? (typeof p.result === "string" ? p.result : p.result?.stdout || JSON.stringify(p.result)) : undefined}
                      stderr={hasResult ? p.result?.stderr : undefined}
                      status={hasResult ? "complete" : "running"}
                    />
                  </motion.div>
                );
              }

              if (category === "file") {
                return (
                  <motion.div key={p.toolCallId} variants={toolCardVariants} initial="hidden" animate="visible">
                    <FileDiffTool
                      toolName={p.toolName}
                      filePath={p.args?.file_path || p.args?.path || p.args?.target_file}
                      content={hasResult ? (typeof p.result === "string" ? p.result : p.args?.content || p.args?.code || p.args?.CodeContent || p.args?.ReplacementContent || JSON.stringify(p.result, null, 2)) : p.args?.content || p.args?.code || p.args?.CodeContent || p.args?.ReplacementContent}
                      status={hasResult ? "complete" : "running"}
                    />
                  </motion.div>
                );
              }

              if (category === "search") {
                return (
                  <motion.div key={p.toolCallId} variants={toolCardVariants} initial="hidden" animate="visible">
                    <SearchTool
                      toolName={p.toolName}
                      query={p.args?.pattern || p.args?.query || p.args?.path}
                      results={hasResult && Array.isArray(p.result) ? p.result : undefined}
                      status={hasResult ? "complete" : "running"}
                    />
                  </motion.div>
                );
              }

              if (category === "permission") {
                return (
                  <motion.div key={p.toolCallId} variants={toolCardVariants} initial="hidden" animate="visible">
                    <PermissionCard
                      toolName={p.toolName}
                      args={p.args}
                      status={hasResult ? "complete" : "running"}
                    />
                  </motion.div>
                );
              }

              // Default fallback for unknown tools
              if (hasResult) {
                return (
                  <motion.div key={p.toolCallId} variants={toolCardVariants} initial="hidden" animate="visible">
                    <ToolResultBlock toolName={p.toolName} result={p.result} />
                  </motion.div>
                );
              }
              return (
                <motion.div key={p.toolCallId} variants={toolCardVariants} initial="hidden" animate="visible">
                  <ToolCallBlock
                    toolName={p.toolName}
                    args={p.args}
                    active={isStreaming}
                  />
                </motion.div>
              );
            }
            return null;
          }}
        </MessagePrimitive.Parts>

        <ActionBarPrimitive.Root className="flex items-center gap-2 mt-4 opacity-0 group-hover:opacity-100 transition-opacity">
          <ActionBarPrimitive.Copy className="text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors px-2 py-1 rounded-[var(--radius-xs)] hover:bg-[var(--bg-hover)]" />
        </ActionBarPrimitive.Root>
      </div>
    </motion.div>
  );
}

/* ============================================================
   User Message
   ============================================================ */
function UserMessage() {
  return (
    <motion.div
      className="group msg-user"
      variants={messageVariants}
      initial="hidden"
      animate="visible"
    >
      <div className="msg-user-bubble">
        <MessagePrimitive.Content />
      </div>

      <ActionBarPrimitive.Root className="flex items-center gap-3 mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
        <ActionBarPrimitive.Copy className="text-[10px] uppercase tracking-wider font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors">
          Copy
        </ActionBarPrimitive.Copy>
      </ActionBarPrimitive.Root>
    </motion.div>
  );
}

/* ============================================================
   Reasoning Block — §2.2: Default collapsed with duration
   ============================================================ */
function ReasoningBlock({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  if (!text.trim()) return null;

  // Estimate duration from text length (rough heuristic for thinking tokens)
  const estimatedSeconds = Math.max(1, Math.round(text.length / 200));

  return (
    <div className="reasoning-block">
      <div
        className="reasoning-header"
        onClick={() => setExpanded(!expanded)}
      >
        <motion.svg
          className="w-3.5 h-3.5"
          animate={{ rotate: expanded ? 90 : 0 }}
          transition={{ duration: 0.2 }}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </motion.svg>
        <span className="font-semibold">THOUGHT</span>
        <span className="opacity-50 text-[10px] ml-1">~{estimatedSeconds}s</span>
        {!expanded && text.length > 60 && (
          <span className="opacity-30 ml-2 truncate font-normal">{text.slice(0, 60)}...</span>
        )}
      </div>
      <AnimatePresence>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: [0.2, 0.8, 0.2, 1] }}
            className="overflow-hidden"
          >
            <div className="p-3 pt-0 text-[var(--text-muted)] text-sm whitespace-pre-wrap font-mono leading-relaxed">
              {text}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

/* ============================================================
   Tool Call Block — Executing state with pulsing skeleton
   ============================================================ */
function ToolCallBlock({ toolName, args, active }: { toolName: string; args: any; active?: boolean }) {
  return (
    <div className={`tool-call-block ${active ? "animate-pulse-subtle border-[var(--accent-emerald)]" : ""}`}>
      <div className="tool-header">
        <svg
          className={`w-3.5 h-3.5 ${active ? "animate-spin" : ""}`}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
        </svg>
        <span className={active ? "font-bold" : ""}>
          {active ? "EXECUTING" : "CALLED"}: {toolName.toUpperCase()}
        </span>
        {active && (
          <motion.span
            className="ml-auto flex gap-0.5"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
          >
            {[0, 1, 2].map((i) => (
              <motion.span
                key={i}
                className="w-1 h-1 rounded-full bg-[var(--accent-emerald)]"
                animate={{ scale: [1, 1.5, 1], opacity: [0.5, 1, 0.5] }}
                transition={{ repeat: Infinity, duration: 1, delay: i * 0.2 }}
              />
            ))}
          </motion.span>
        )}
      </div>
      <div className="p-3 pt-0 font-mono text-[11px] text-[var(--text-muted)] opacity-70">
        {JSON.stringify(args, null, 2)}
      </div>
    </div>
  );
}

/* ============================================================
   Tool Result Block — Collapsible with copy action
   ============================================================ */
function ToolResultBlock({ toolName, result }: { toolName: string; result: any }) {
  const [expanded, setExpanded] = useState(false);
  const resultStr = typeof result === "string" ? result : JSON.stringify(result, null, 2);

  return (
    <div className="tool-call-block border-[var(--border-bright)] bg-[var(--bg-elevated)] overflow-hidden">
      <div className="flex items-center">
        <div
          className="flex-1 tool-header cursor-pointer hover:bg-[var(--bg-hover)] transition-colors"
          onClick={() => setExpanded(!expanded)}
        >
          <svg className="w-3.5 h-3.5 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
          </svg>
          <span className="text-[var(--accent-emerald)]">RESULT: {toolName.toUpperCase()}</span>
          <motion.svg
            className="ml-auto w-3.5 h-3.5"
            animate={{ rotate: expanded ? 90 : 0 }}
            transition={{ duration: 0.2 }}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </motion.svg>
        </div>

        <button
          onClick={() => navigator.clipboard.writeText(resultStr)}
          className="p-2 text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors border-l border-[var(--border-subtle)]"
          title="Copy result"
        >
          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
          </svg>
        </button>
      </div>

      <AnimatePresence>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: [0.2, 0.8, 0.2, 1] }}
            className="overflow-hidden"
          >
            <div className="p-3 pt-0 font-mono text-[11px] text-[var(--text-secondary)] whitespace-pre-wrap border-t border-[var(--border-subtle)] mt-1">
              {resultStr}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

/* ============================================================
   Composer
   ============================================================ */
function Composer() {
  return (
    <ComposerPrimitive.Root className="composer-root">
      <div className="composer-input-row">
        <ComposerPrimitive.Input
          className="composer-input"
          rows={1}
          autoFocus
          placeholder="Type a message or '/' for commands..."
        />

        <div className="flex items-center gap-2">
          <ComposerPrimitive.Cancel
            className="btn-icon text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.1)]"
            title="Stop"
          >
            <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
              <rect x="6" y="6" width="12" height="12" rx="2" />
            </svg>
          </ComposerPrimitive.Cancel>

          <ComposerPrimitive.Send className="btn-icon btn-primary" disabled>
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 12h14M12 5l7 7-7 7" />
            </svg>
          </ComposerPrimitive.Send>
        </div>
      </div>
    </ComposerPrimitive.Root>
  );
}
