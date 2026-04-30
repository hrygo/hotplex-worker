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
  
  const isWaiting = isRunning && lastMessage?.role === 'user';

  if (!isWaiting) return null;

  return (
    <motion.div
      className="group msg-assistant flex items-start gap-4 mb-8"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, ease: "easeOut" }}
    >
      <div className="flex-shrink-0">
        <div className="w-9 h-9 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] flex items-center justify-center border border-[var(--border-subtle)] relative overflow-hidden">
          <div className="absolute inset-0 bg-gradient-to-tr from-[var(--accent-gold)]/10 to-transparent animate-pulse" />
          <BrandIcon size={24} className="opacity-40 animate-pulse-subtle" />
        </div>
      </div>

      <div className="msg-assistant-body flex flex-col gap-3">
        <div className="flex items-center gap-3 mb-1">
          <div className="flex items-center gap-1.5">
            <span className="thinking-dot" />
            <span className="thinking-dot" style={{ animationDelay: '0.2s' }} />
            <span className="thinking-dot" style={{ animationDelay: '0.4s' }} />
          </div>
          <span className="text-[11px] font-display font-bold text-[var(--accent-gold)] tracking-[0.1em] uppercase">
            Synthesizing Strategy
          </span>
        </div>
        <div className="flex flex-col gap-3 max-w-sm">
          <div className="skeleton-text w-full h-2 rounded-[var(--radius-xs)] animate-shimmer" />
          <div className="skeleton-text w-[92%] h-2 rounded-[var(--radius-xs)] animate-shimmer" style={{ animationDelay: '0.15s' }} />
          <div className="skeleton-text w-[78%] h-2 rounded-[var(--radius-xs)] animate-shimmer" style={{ animationDelay: '0.3s' }} />
        </div>
      </div>
    </motion.div>
  );
}

/* ============================================================
   Assistant Message - Enhanced with Functional Regression
   ============================================================ */
function CopyButton({ message, onCopy }: { message: any, onCopy?: () => void }) {
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
      onCopy?.();
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <button onClick={handleCopy} className={`copy-btn ${copied ? 'copy-btn-success' : ''}`}>
      <AnimatePresence mode="wait" initial={false}>
        {copied ? (
          <motion.div key="check" initial={{ y: 2, opacity: 0 }} animate={{ y: 0, opacity: 1 }} exit={{ y: -2, opacity: 0 }} className="flex items-center gap-1.5">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
            </svg>
            <span>COPIED</span>
          </motion.div>
        ) : (
          <motion.div key="copy" initial={{ y: 2, opacity: 0 }} animate={{ y: 0, opacity: 1 }} exit={{ y: -2, opacity: 0 }} className="flex items-center gap-1.5">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" />
            </svg>
            <span>COPY</span>
          </motion.div>
        )}
      </AnimatePresence>
    </button>
  );
}

function MessageActions({ message, isUser }: { message: any, isUser?: boolean }) {
  return (
    <div className={`message-action-bar ${isUser ? 'justify-end' : 'justify-start'}`}>
      <CopyButton message={message} />
    </div>
  );
}

function AssistantMessage({ message }: { message: any }) {
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});

  return (
    <motion.div className="group msg-assistant flex items-start gap-4 mb-8" variants={messageVariants} initial="hidden" animate="visible">
      <div className="flex-shrink-0">
        <div className="w-9 h-9 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] shadow-sm flex items-center justify-center relative overflow-hidden">
          <div className="absolute inset-0 bg-gradient-to-br from-[var(--accent-gold)]/5 to-transparent" />
          <BrandIcon size={24} />
        </div>
      </div>

      <div className="flex flex-col flex-1 min-w-0">
        <div className="msg-assistant-body relative p-0 space-y-4">
          <MessagePrimitive.Parts>
            {({ part }) => {
              const p = part as Record<string, any>;
              if (!p || !p.type) return null;
              const isStreaming = (message as any)?.status?.type === "running";

              if (p.type === "reasoning") return <ReasoningBlock text={p.text} />;
              if (p.type === "text") return <div className={`prose-hotplex ${isStreaming ? "streaming-cursor" : ""}`}><MarkdownText text={p.text} /></div>;
              
              if (p.type === "tool-call") {
                const parts = (message.content as any[]) || [];
                const partIndex = parts.indexOf(p);
                const isLastPart = partIndex === parts.length - 1;
                const isComplete = p.status?.type === "complete" || p.status?.type === "error";
                const isExpanded = !!expandedTools[p.toolCallId || partIndex];
                
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
                  <motion.div 
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    className="relative mt-3 first:mt-0"
                  >
                    {(() => {
                      switch (category) {
                        case "terminal": return <TerminalTool command={extractCommand(args)} stdout={p.result?.stdout || (typeof p.result === 'string' ? p.result : '')} stderr={p.result?.stderr} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "file": return <FileDiffTool toolName={p.toolName} filePath={extractFilePath(args)} content={extractFileContent(args, p.result)} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "search": return <SearchTool toolName={p.toolName} query={args.query || args.pattern} results={p.result} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "list": return <ListTool toolName={p.toolName} path={extractFilePath(args)} items={p.result} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "permission": return <PermissionCard toolName={p.toolName} args={args} status={status === "error" ? "complete" : status as "running" | "complete"} onToggle={!isLastPart ? toggle : undefined} />;
                        default: return <div className="p-4 bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-[var(--radius-md)] font-mono text-[11px] mt-2 shadow-inner">{JSON.stringify(p.result || args, null, 2)}</div>;
                      }
                    })()}
                  </motion.div>
                );
              }
              if (p.type === "tool-summary") {
                const names = p.toolNames || [];
                return (
                  <div className="flex items-center gap-2 px-3 py-1.5 mt-3 rounded-[var(--radius-sm)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[11px] font-bold text-[var(--text-secondary)] w-fit shadow-sm">
                    <span className="text-[var(--accent-gold)] animate-pulse-subtle">🔧</span>
                    <span className="tracking-wide">{names.join(', ').toUpperCase()}</span>
                    {p.count > 1 && <span className="text-[var(--text-faint)] ml-1">×{p.count}</span>}
                  </div>
                );
              }
              return null;
            }}
          </MessagePrimitive.Parts>
        </div>
        <MessageActions message={message} />
      </div>
    </motion.div>
  );
}

/* ============================================================
   Rest of the components (UserMessage, ReasoningBlock, Composer, etc.)
   Keeping original visual structure but ensuring logic compatibility.
   ============================================================ */

function ReasoningBlock({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(true);
  if (!text.trim()) return null;
  const estimatedSeconds = Math.max(1, Math.round(text.length / 200));

  return (
    <div className="reasoning-block group/reasoning border-[var(--border-subtle)] hover:border-[var(--accent-gold)]/20 transition-colors">
      <div 
        className="reasoning-header px-4 py-2.5 flex items-center gap-3 cursor-pointer select-none" 
        onClick={() => setExpanded(!expanded)}
      >
        <motion.div 
          animate={{ rotate: expanded ? 90 : 0 }}
          transition={{ type: "spring", stiffness: 400, damping: 30 }}
        >
          <svg className="w-3.5 h-3.5 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M9 5l7 7-7 7" />
          </svg>
        </motion.div>
        <span className="text-[11px] font-display font-bold tracking-[0.1em] text-[var(--text-secondary)]">THOUGHT PROCESS</span>
        <div className="flex-1 h-[1px] bg-gradient-to-r from-[var(--border-subtle)] to-transparent" />
        <span className="font-mono text-[10px] text-[var(--text-faint)] tabular-nums">
          {estimatedSeconds}s elapsed
        </span>
      </div>
      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div 
            initial={{ height: 0, opacity: 0 }} 
            animate={{ height: "auto", opacity: 1 }} 
            exit={{ height: 0, opacity: 0 }} 
            transition={{ duration: 0.3, ease: "easeInOut" }}
            className="overflow-hidden"
          >
            <div className="reasoning-content border-t border-[var(--border-subtle)]/50 leading-relaxed">
              {text}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function UserMessage({ message }: { message: any }) {
  return (
    <motion.div className="group flex items-start justify-end gap-4 mb-8" variants={messageVariants} initial="hidden" animate="visible">
      <div className="relative max-w-[85%] flex-1 flex flex-col items-end min-w-0 group/msg">
        <div className="msg-user-bubble relative w-fit p-3.5 rounded-[var(--radius-lg)] rounded-tr-[var(--radius-xs)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] shadow-sm">
          <MessagePrimitive.Parts>
            {({ part }) => {
              const p = part as Record<string, any>;
              if (p?.type === 'text') return <div className="whitespace-pre-wrap break-normal text-[14px] leading-relaxed">{p.text}</div>;
              return null;
            }}
          </MessagePrimitive.Parts>
        </div>
        <MessageActions message={message} isUser />
      </div>
      <div className="flex-shrink-0">
        <div className="w-9 h-9 rounded-full bg-[var(--bg-elevated)] border border-[var(--border-subtle)] flex items-center justify-center shadow-sm">
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
      <div className="relative mb-10 flex items-center justify-center">
        <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-10 blur-[100px] rounded-full scale-[2.5]" />

        {/* Orbital rings */}
        <div className="absolute w-36 h-36 border border-[var(--accent-gold)] opacity-20 rounded-full animate-[spin_12s_linear_infinite]" />
        <div className="absolute w-44 h-44 border border-[var(--accent-emerald)] opacity-10 rounded-full animate-[spin_18s_linear_infinite_reverse]" />

        {/* Orbiting particles */}
        <div className="absolute w-full h-full animate-[orbit_5s_linear_infinite]">
           <div className="w-2.5 h-2.5 rounded-full bg-[var(--accent-gold)] shadow-[0_0_10px_var(--accent-gold)]" />
        </div>

        <BrandIcon size={112} className="relative z-10 animate-float" />
      </div>
      <h1 className="text-5xl font-display font-bold tracking-tight mb-4 text-[var(--text-primary)]">HotPlex</h1>
      <p className="text-xl text-[var(--text-muted)] font-medium max-w-lg mx-auto leading-relaxed">
        Next-generation autonomous workspace.
      </p>
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
          <ThreadPrimitive.ScrollToBottom className="scroll-bottom-btn">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
            </svg>
            <span>New</span>
          </ThreadPrimitive.ScrollToBottom>
          <PreAssistantIndicator />
        </div>
      </ThreadPrimitive.Viewport>

      <div className="composer-wrapper px-4 pb-12">
        <div className="composer-container relative max-w-4xl mx-auto">
          <AnimatePresence>
            {menuOpen && <CommandMenu isOpen={menuOpen} inputValue={localText} onSelect={handleSelectCommand} onClose={() => setMenuOpen(false)} skills={skills} />}
          </AnimatePresence>
          <div className="flex items-center gap-2 mb-3 px-4 overflow-x-auto no-scrollbar animate-fadeIn">
            <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-[var(--accent-gold)]/10 border border-[var(--accent-gold)]/20 shadow-sm whitespace-nowrap">
              <span className="text-[9px] font-display font-black text-[var(--accent-gold)] uppercase tracking-[0.05em]">Agent Skills</span>
              <div className="w-1 h-1 rounded-full bg-[var(--accent-gold)] animate-pulse" />
            </div>
            {skills?.slice(0, 4).map(skill => (
              <div key={skill} className="px-3 py-1 rounded-full bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[10px] font-medium text-[var(--text-muted)] whitespace-nowrap hover:border-[var(--text-faint)] transition-colors cursor-default">
                {skill}
              </div>
            ))}
            {skills && skills.length > 4 && (
              <div className="px-2 py-1 text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-tighter">
                +{skills.length - 4} More
              </div>
            )}
          </div>

          <div className="relative">
            <div className="absolute bottom-full left-1/2 -translate-x-1/2 z-20 mb-0">
              <ThreadPrimitive.ScrollToBottom className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-[var(--bg-surface)] border border-[var(--border-bright)] shadow-xl text-[var(--accent-gold)] hover:bg-[var(--bg-hover)] hover:border-[var(--accent-gold)] transition-all active:scale-95 group/scroll">
                <svg className="w-3.5 h-3.5 animate-bounce-subtle" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
                </svg>
                <span className="text-[10px] font-bold uppercase tracking-widest">Latest Messages</span>
              </ThreadPrimitive.ScrollToBottom>
            </div>

            <ComposerPrimitive.Root className="composer-root shadow-[0_20px_50px_rgba(0,0,0,0.3)]">
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
        </div>
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
