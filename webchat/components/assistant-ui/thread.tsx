"use client";

import React, { useCallback, useRef, useState, memo } from "react";
import {
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
} from "@assistant-ui/react";
import { useAui, useAuiState } from "@assistant-ui/store";
import { motion, AnimatePresence } from "framer-motion";
import { MarkdownText } from "./MarkdownText";
import { TerminalTool } from "./tools/TerminalTool";
import { FileDiffTool } from "./tools/FileDiffTool";
import { SearchTool } from "./tools/SearchTool";
import { PermissionCard } from "./tools/PermissionCard";
import { BrandIcon } from "@/components/icons";
import { getToolCategory } from "@/lib/tool-categories";
import { CommandMenu } from "./CommandMenu";
import { CompactToolTab } from "./tools/CompactToolTab";
import { ListTool } from "./tools/ListTool";

/* ============================================================
   Animation & Extraction Logic (Legacy Sync)
   ============================================================ */
const messageVariants = {
  hidden: { opacity: 0, y: 10 },
  visible: { opacity: 1, y: 0, transition: { type: "spring" as const, stiffness: 300, damping: 24 } },
};

function extractCommand(args: any) { return args?.command || args?.Command || ""; }
function extractFilePath(args: any) { return args?.file_path || args?.path || args?.target_file; }
function extractFileContent(args: any, result: any) { return args?.content || args?.code || (typeof result === "string" ? result : ""); }

/* ============================================================
   Pre-Assistant Indicator — § Instant feedback before bot object exists
   ============================================================ */
function PreAssistantIndicator() {
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const messages = useAuiState((s) => s.thread.messages);
  const lastMessage = messages[messages.length - 1];
  
  // Show if thread is busy but no assistant message has appeared yet for the current turn
  const isWaiting = isRunning && lastMessage?.role === 'user';

  if (!isWaiting) return null;

  return (
    <motion.div
      className="group msg-assistant flex items-start gap-6 mb-12"
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
    >
      <div className="flex-shrink-0">
        <div className="relative">
          <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-20 blur-md rounded-xl animate-pulse-subtle" />
          <div className="w-10 h-10 rounded-xl glass-dark flex items-center justify-center border border-[var(--border-bright)] relative z-10 shadow-lg">
            <BrandIcon size={30} />
          </div>
        </div>
      </div>

      <div className="msg-assistant-body active-session shadow-[0_0_50px_rgba(251,191,36,0.05)]">
        <div className="flex items-start gap-4 px-2">
          <div className="relative w-8 h-8 flex-shrink-0 mt-1" style={{ perspective: '400px' }}>
            <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-20 blur-xl rounded-full animate-pulse-bloom" style={{ animationDuration: '3s' }} />
            <svg className="absolute inset-[-4px] w-[calc(100%+8px)] h-[calc(100%+8px)] pointer-events-none" style={{ transform: 'rotateX(65deg) rotateY(0deg)', animation: 'rotateOrbit 4s linear infinite' }}>
              <ellipse cx="50%" cy="50%" rx="12" ry="12" fill="none" stroke="var(--accent-gold)" strokeWidth="0.5" strokeDasharray="1 3" opacity="0.3" />
              <circle r="1.5" fill="var(--accent-gold)">
                <animateMotion dur="2s" repeatCount="indefinite" path="M -12,0 a 12,12 0 1,0 24,0 a 12,12 0 1,0 -24,0" />
              </circle>
            </svg>
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="w-1.5 h-1.5 rounded-full bg-[var(--accent-gold)] shadow-[0_0_8px_var(--accent-gold)] animate-quantum-wobble" />
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[10px] font-mono text-[var(--accent-gold)] font-bold tracking-[0.2em] animate-pulse uppercase">
              Thinking
            </span>
            <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-wider">
              Preparing response...
            </span>
          </div>
        </div>
      </div>
    </motion.div>
  );
}

/* ============================================================
   Assistant Message - Enhanced with Functional Regression
   ============================================================ */
function CopyButton({ message }: { message: any }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    let text = "";
    if (typeof message.content === 'string') {
      text = message.content;
    } else if (Array.isArray(message.content)) {
      text = message.content.map((p: any) => p.text || "").filter(Boolean).join("\n\n");
    }
    
    if (text) {
      navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <button onClick={handleCopy} className={`copy-btn ${copied ? 'copy-btn-success' : ''}`}>
      <AnimatePresence mode="wait">
        {copied ? (
          <motion.div key="check" initial={{ scale: 0.5, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} exit={{ scale: 0.5, opacity: 0 }} className="flex items-center gap-1.5">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" /></svg>
            <span>Copied</span>
          </motion.div>
        ) : (
          <motion.div key="copy" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="flex items-center gap-1.5">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" /></svg>
            <span>Copy</span>
          </motion.div>
        )}
      </AnimatePresence>
    </button>
  );
}

function AssistantMessage({ message }: { message: any }) {
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});

  return (
    <motion.div className="group msg-assistant" variants={messageVariants} initial="hidden" animate="visible">
      <div className="flex-shrink-0">
        <div className="w-9 h-9 rounded-[var(--radius-md)] glass-dark flex items-center justify-center">
          <BrandIcon size={28} />
        </div>
      </div>

      <div className="msg-assistant-body relative">
        <div className="message-actions" style={{ right: '4px', top: '-14px' }}>
          <CopyButton message={message} />
        </div>
        <MessagePrimitive.Parts>
          {({ part }) => {
            const p = part as Record<string, any>;
            if (!p || !p.type) return null;
            const isStreaming = (message as any)?.status?.type === "running";

            if (p.type === "reasoning") return <ReasoningBlock text={p.text} />;
            if (p.type === "text") return <span className={isStreaming ? "streaming-cursor" : ""}><MarkdownText text={p.text} /></span>;
            
            if (p.type === "tool-call") {
              const parts = (message.content as any[]) || [];
              const partIndex = parts.indexOf(p);
              const isLastPart = partIndex === parts.length - 1;
              const isComplete = p.status?.type === "complete" || p.status?.type === "error";
              const isExpanded = !!expandedTools[p.toolCallId || partIndex];
              
              // Functional Enhancement: Auto-compact non-last finished tools
              const isCompacted = !isLastPart && isComplete && !isExpanded;
              const toggle = () => setExpandedTools(prev => ({ ...prev, [p.toolCallId || partIndex]: !prev[p.toolCallId || partIndex] }));

              const category = getToolCategory(p.toolName);
              const args = p.args ?? {};
              const status = isComplete ? (p.status?.type === "error" ? "error" : "complete") : "running";

              if (isCompacted) {
                return (
                  <CompactToolTab 
                    toolName={p.toolName} 
                    summary={extractCommand(args) || extractFilePath(args) || "Action..."} 
                    status={status === "running" ? "complete" : status as "complete" | "error"} 
                    onClick={toggle} 
                  />
                );
              }

              return (
                <div className="relative">
                  {(() => {
                    switch (category) {
                      case "terminal": return <TerminalTool command={extractCommand(args)} stdout={p.result?.stdout || (typeof p.result === 'string' ? p.result : '')} stderr={p.result?.stderr} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                      case "file": return <FileDiffTool toolName={p.toolName} filePath={extractFilePath(args)} content={extractFileContent(args, p.result)} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                      case "search": return <SearchTool toolName={p.toolName} query={args.query || args.pattern} results={p.result} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                      case "list": return <ListTool toolName={p.toolName} path={extractFilePath(args)} items={p.result} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                      case "permission": return <PermissionCard toolName={p.toolName} args={args} status={status === "error" ? "complete" : status as "running" | "complete"} onToggle={!isLastPart ? toggle : undefined} />;
                      default: return <div className="p-3 bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-md font-mono text-[11px] mt-4">{JSON.stringify(p.result || args, null, 2)}</div>;
                    }
                  })()}
                </div>
              );
            }
            return null;
          }}
        </MessagePrimitive.Parts>
      </div>
    </motion.div>
  );
}

/* ============================================================
   Rest of the components (UserMessage, ReasoningBlock, Composer, etc.)
   Keeping original visual structure but ensuring logic compatibility.
   ============================================================ */

function ReasoningBlock({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  if (!text.trim()) return null;
  const estimatedSeconds = Math.max(1, Math.round(text.length / 200));

  return (
    <div className="reasoning-block">
      <div className="reasoning-header" onClick={() => setExpanded(!expanded)}>
        <motion.svg className="w-3.5 h-3.5" animate={{ rotate: expanded ? 90 : 0 }} fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M9 5l7 7-7 7" /></motion.svg>
        <span>THOUGHT</span>
        <span className="opacity-40 font-mono text-[9px] ml-auto tracking-normal">~{estimatedSeconds}s</span>
      </div>
      <AnimatePresence>
        {expanded && (
          <motion.div initial={{ height: 0, opacity: 0 }} animate={{ height: "auto", opacity: 1 }} exit={{ height: 0, opacity: 0 }} className="overflow-hidden">
            <div className="reasoning-content">{text}</div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function UserMessage({ message }: { message: any }) {
  return (
    <motion.div className="group flex items-start justify-end gap-4 mb-8" variants={messageVariants} initial="hidden" animate="visible">
      <div className="flex flex-col items-end max-w-[85%] relative">
        <div className="message-actions" style={{ right: '4px', top: '-14px' }}>
          <CopyButton message={message} />
        </div>
        <div className="msg-user-bubble w-full"><MessagePrimitive.Content /></div>
      </div>
      <div className="flex-shrink-0 mt-0.5">
        <div className="w-9 h-9 rounded-full glass-dark flex items-center justify-center border border-[var(--border-subtle)]">
          <svg className="w-5 h-5 text-[var(--text-muted)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
          </svg>
        </div>
      </div>
    </motion.div>
  );
}

function WelcomeScreen() {
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
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
      <h1 className="text-4xl font-display font-bold tracking-tight mb-3 text-[var(--text-primary)]">HotPlex Workbench</h1>
      <p className="text-lg text-[var(--text-muted)] mb-12 max-w-lg mx-auto">Autonomous workspace powered by HotPlex Intelligence.</p>
    </div>
  );
}

interface ThreadProps {
  skills?: string[];
  hasMore?: boolean;
  onLoadHistory?: () => Promise<{ hasMore: boolean }>;
}

export function Thread({ skills, hasMore, onLoadHistory }: ThreadProps) {
  const [localText, setLocalText] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [loadingHistory, setLoadingHistory] = useState(false);
  const [historyHasMore, setHistoryHasMore] = useState(hasMore);
  const aui = useAui();
  const text = useAuiState((s) => s.composer.text);
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const composingRef = useRef(false);

  React.useEffect(() => { if (!composingRef.current) { setLocalText(text || ""); if (!text) setMenuOpen(false); } }, [text]);

  const handleCompositionStart = useCallback(() => { composingRef.current = true; }, []);
  const handleCompositionEnd = useCallback((e: React.CompositionEvent<HTMLTextAreaElement>) => {
    composingRef.current = false;
    const val = e.currentTarget.value;
    setLocalText(val);
    aui.composer().setText(val);
  }, [aui]);

  const handleChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const val = e.target.value;
    setLocalText(val);
    setMenuOpen(val.startsWith("/"));
    if (!composingRef.current) aui.composer().setText(val);
  }, [aui]);

  const handleSelectCommand = useCallback((cmd: string) => {
    setLocalText(cmd);
    aui.composer().setText(cmd);
    setMenuOpen(false);
  }, [aui]);

  const handleLoadEarlier = useCallback(async () => {
    if (!onLoadHistory || loadingHistory) return;
    setLoadingHistory(true);
    try {
      const result = await onLoadHistory();
      setHistoryHasMore(result.hasMore);
    } finally {
      setLoadingHistory(false);
    }
  }, [onLoadHistory, loadingHistory]);

  return (
    <ThreadPrimitive.Root className="flex flex-col h-full relative overflow-hidden bg-[var(--bg-base)]">
      <ThreadPrimitive.Viewport className="thread-viewport relative px-4 py-8">
        <div className="max-w-5xl mx-auto w-full">
          <ThreadPrimitive.Empty><WelcomeScreen /></ThreadPrimitive.Empty>
          {historyHasMore && (
            <div className="flex justify-center py-4 mb-4">
              <button
                onClick={handleLoadEarlier}
                disabled={loadingHistory}
                className="px-4 py-2 text-[11px] font-mono uppercase tracking-wider font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-full hover:bg-[var(--bg-hover)] transition-all active:scale-95 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {loadingHistory ? (
                  <span className="flex items-center gap-2">
                    <svg className="w-3 h-3 animate-spin" viewBox="0 0 24 24" fill="none">
                      <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeDasharray="31.4 31.4" strokeLinecap="round" />
                    </svg>
                    Loading...
                  </span>
                ) : "Load earlier messages"}
              </button>
            </div>
          )}
          <ThreadPrimitive.Messages>
            {({ message }) =>
              message.role === "user" ? <UserMessage message={message} /> : <AssistantMessage message={message} />
            }
          </ThreadPrimitive.Messages>
          <PreAssistantIndicator />
        </div>
      </ThreadPrimitive.Viewport>

      <div className="composer-wrapper px-4 pb-12">
        <div className="composer-container relative max-w-4xl mx-auto">
          <AnimatePresence>
            {menuOpen && <CommandMenu isOpen={menuOpen} inputValue={localText} onSelect={handleSelectCommand} onClose={() => setMenuOpen(false)} skills={skills} />}
          </AnimatePresence>
          <ComposerPrimitive.Root className="composer-root">
            <div className="composer-input-row">
              <ComposerPrimitive.Input 
                className="composer-input" 
                rows={1} 
                autoFocus 
                submitMode="enter" 
                placeholder="Type a message or '/' for commands..."
                value={localText} 
                onChange={handleChange} 
                onCompositionStart={handleCompositionStart} 
                onCompositionEnd={handleCompositionEnd} 
              />
              <div className="flex items-center gap-2">
                {isRunning && <ComposerPrimitive.Cancel className="btn-icon"><svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="2" /></svg></ComposerPrimitive.Cancel>}
                <ComposerPrimitive.Send className="btn-icon btn-primary"><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 12h14M12 5l7 7-7 7" /></svg></ComposerPrimitive.Send>
              </div>
            </div>
          </ComposerPrimitive.Root>
          <div className="mt-2 flex justify-between items-center px-2">
            <div className="flex gap-4">
              <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
                <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Enter</kbd> to send
              </span>
              <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
                <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Shift</kbd> + <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Enter</kbd> new line
              </span>
            </div>
            <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest">
              v1.1.0-stable
            </span>
          </div>
        </div>
      </div>
    </ThreadPrimitive.Root>
  );
}
