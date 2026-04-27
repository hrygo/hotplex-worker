"use client";

import React, { useCallback, useRef, useState } from "react";
import { useCopyToClipboard } from "@/lib/hooks/useCopyToClipboard";
import {
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ActionBarPrimitive,
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
import { ListTool } from "./tools/ListTool";
import { TodoTool } from "./tools/TodoTool";

/* ============================================================
   Animation variants
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
   Tool prop extractors — avoid JSON.stringify in render path
   ============================================================ */
function extractCommand(args: Record<string, any>): string {
  return args?.command || args?.Command || Object.values(args).join(" ");
}

function extractTerminalOutput(result: any): { stdout?: string; stderr?: string } {
  if (typeof result === "string") return { stdout: result };
  return { stdout: result?.stdout, stderr: result?.stderr };
}

function extractFilePath(args: Record<string, any>): string | undefined {
  return args?.file_path || args?.path || args?.target_file || args?.TargetFile;
}

function extractFileContent(args: Record<string, any>, result: any): string | undefined {
  const fromArgs = args?.content || args?.code || args?.CodeContent || args?.ReplacementContent;
  if (fromArgs) return fromArgs;
  if (typeof result === "string") return result;
  if (typeof result === "object") return result?.content || result?.output || result?.text;
  return undefined;
}

function extractSearchQuery(args: Record<string, any>): string | undefined {
  return args?.pattern || args?.query || args?.path;
}

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
      className="group msg-assistant flex items-start gap-6 mb-12 animate-fade-in"
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
    >
      <div className="flex-shrink-0" style={{ transform: 'translateY(0.5rem)' }}>
        <div className="relative">
          <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-20 blur-md rounded-xl animate-pulse-subtle" />
          <div className="w-10 h-10 rounded-xl glass-dark flex items-center justify-center border border-[var(--border-bright)] relative z-10 shadow-lg">
            <BrandIcon size={30} />
          </div>
        </div>
      </div>

      <div className="msg-assistant-body active-session shadow-[0_0_50px_rgba(251,191,36,0.05)]">
        <div className="flex items-center gap-4 py-4 px-2">
          <div className="relative w-8 h-8 flex-shrink-0" style={{ perspective: '400px' }}>
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
              Quantum Thinking
            </span>
            <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-wider">
              Establishing Neural Link...
            </span>
          </div>
        </div>
      </div>
    </motion.div>
  );
}

/* ============================================================
   Thread — Main conversation wrapper
   ============================================================ */
export function Thread({ skills }: { skills?: string[] }) {
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
            {({ message }: { message: any }) =>
              message.role === "user" ? <UserMessage /> : <AssistantMessage />
            }
          </ThreadPrimitive.Messages>

          {/* Instant Response Placeholder — § Removes the 10s black hole gap */}
          <PreAssistantIndicator />
        </div>
      </ThreadPrimitive.Viewport>

      {/* Composer wrapper (floating footer) */}
      <div className="composer-wrapper">
        <div className="composer-container relative">
          <ThreadPrimitive.ScrollToBottom asChild>
            <button className="scroll-bottom-btn">
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M19 14l-7 7-7-7" />
              </svg>
              <span>Jump to latest</span>
            </button>
          </ThreadPrimitive.ScrollToBottom>
          
          <Composer skills={skills} />
          <div className="mt-2 text-center">
            <p className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest">
              Enter to send · Shift + Enter for new line
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
      className="flex flex-col items-center justify-center py-12 text-center"
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.8, ease: [0.16, 1, 0.3, 1] }}
    >
      <div className="relative mb-14 flex items-center justify-center scale-90 sm:scale-100" style={{ perspective: '1500px' }}>
        {/* Deep Atmospheric Bloom */}
        <div className="absolute inset-[-140px] bg-[var(--accent-gold)] rounded-full animate-pulse-bloom pointer-events-none" />
        
        {/* SVG Quantum Energy Field */}
        <svg className="absolute inset-[-120px] w-[calc(100%+240px)] h-[calc(100%+240px)] pointer-events-none overflow-visible" style={{ transform: 'rotateX(65deg) rotateY(0deg)', animation: 'rotateOrbit 25s linear infinite' }}>
          <defs>
            <filter id="glow-lg">
              <feGaussianBlur stdDeviation="2" result="coloredBlur"/>
              <feMerge>
                <feMergeNode in="coloredBlur"/><feMergeNode in="SourceGraphic"/>
              </feMerge>
            </filter>
          </defs>

          {/* Orbit 1 — High Density Trails */}
          <ellipse cx="50%" cy="50%" rx="120" ry="120" fill="none" stroke="var(--accent-gold)" strokeWidth="0.5" strokeDasharray="5 15" opacity="0.2" />
          {[0, 0.08, 0.16, 0.24].map((d) => (
            <circle key={`e1-t-${d}`} r={3.5 - d * 6} fill="var(--accent-gold)" opacity={1 - d * 3} filter="url(#glow-lg)">
              <animateMotion dur="8s" begin={`${d}s`} repeatCount="indefinite" path="M -120,0 a 120,120 0 1,0 240,0 a 120,120 0 1,0 -240,0" />
            </circle>
          ))}

          {/* Orbit 2 — Violet Swarm */}
          <ellipse cx="50%" cy="50%" rx="140" ry="140" fill="none" stroke="var(--accent-violet)" strokeWidth="0.5" strokeDasharray="3 12" opacity="0.15" />
          {[0, 0.12, 0.24, 0.36].map((d) => (
            <circle key={`e2-t-${d}`} r={3 - d * 5} fill="var(--accent-violet)" opacity={0.8 - d * 2} filter="url(#glow-lg)">
              <animateMotion dur="12s" begin={`${d}s`} repeatCount="indefinite" path="M -140,0 a 140,140 0 1,0 280,0 a 140,140 0 1,0 -280,0" />
            </circle>
          ))}

          {/* Background Ambient Cloud */}
          {[100, 130, 160, 190, 220].map((r, i) => (
            <circle key={`amb-${i}`} r="1.5" fill="white" opacity="0.1" filter="url(#glow-lg)">
              <animateMotion dur={`${20 + i * 8}s`} begin={`${i * 3}s`} repeatCount="indefinite" path={`M -${r},0 a ${r},${r} 0 1,0 ${r*2},0 a ${r},${r} 0 1,0 -${r*2},0`} />
            </circle>
          ))}
        </svg>

        {/* Nucleus Logo with Magnetic Float */}
        <div className="relative z-10 flex items-center justify-center animate-float animate-quantum-wobble">
          <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-30 blur-3xl rounded-full scale-150 animate-pulse-bloom transform-gpu" style={{ animationDuration: '3s' }} />
          <BrandIcon size={120} className="relative z-20 drop-shadow-[0_0_40px_rgba(251,191,36,0.6)]" />
        </div>
      </div>

      <h1 className="text-3xl sm:text-4xl font-display font-bold tracking-tight mb-10 text-gradient-gold">
        HotPlex Intelligence
      </h1>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 w-full max-w-2xl mx-auto px-4">
        {SUGGESTIONS.map((s, i) => {
          const selfContained = s.icon === "code";
          return (
            <ThreadPrimitive.Suggestion
              key={s.prompt}
              prompt={s.prompt}
              {...(selfContained ? { send: true } : {})}
              className="suggestion-card flex items-center gap-4 p-5 rounded-2xl bg-gradient-to-b from-white/[0.03] to-transparent border border-white/[0.05] hover:border-[var(--border-gold)] transition-all duration-500 group backdrop-blur-md"
            >
              <motion.div
                className="w-10 h-10 rounded-xl bg-[var(--bg-elevated)] flex items-center justify-center text-[var(--accent-gold)] group-hover:scale-110 transition-transform shadow-lg"
                initial={{ opacity: 0, scale: 0.8 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={{ delay: 0.2 + i * 0.1 }}
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={ICON_PATHS[s.icon]} />
                </svg>
              </motion.div>
              <div className="text-left">
                <span className="block text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-[0.2em] mb-0.5 group-hover:text-[var(--accent-gold)] transition-colors">{s.icon}</span>
                <span className="text-[15px] font-bold text-[var(--text-primary)] group-hover:translate-x-1 transition-transform block">
                  {s.prompt}
                </span>
              </div>
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
  const message = useAuiState((s) => s.message);
  const isStreaming = message.status?.type === "running";

  // Optimized lifecycle: check if message is effectively empty to avoid layout shifts
  const isEffectivelyEmpty = !message.content || message.content.length === 0 || 
    message.content.every(p => (p.type === "text" || p.type === "reasoning") && !(p as any).text?.trim());

  const firstPart = message.content?.[0] as any;
  const isReasoning = firstPart?.type === "reasoning";
  const isPlaceholder = isEffectivelyEmpty;
  
  // Offsets calculated to align 40px avatar center with first line center:
  // - Reasoning header center (~35px height) needs ~-2.4px (-0.15rem)
  // - Markdown line center (~26px height) needs ~-7px (-0.43rem)
  // - Streaming indicator center (~56px height) needs ~8px (0.5rem)
  const avatarOffset = isPlaceholder ? "0.5rem" : (isReasoning ? "-0.15rem" : "-0.43rem");

  return (
    <motion.div
      className="group msg-assistant flex items-start gap-6 mb-12"
      variants={messageVariants}
      initial="hidden"
      animate="visible"
    >
      <div className="flex-shrink-0" style={{ transform: `translateY(${avatarOffset})` }}>
        <div className="relative">
          <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-20 blur-md rounded-xl animate-pulse-subtle" />
          <div className="w-10 h-10 rounded-xl glass-dark flex items-center justify-center border border-[var(--border-bright)] relative z-10 shadow-lg">
            <BrandIcon size={30} />
          </div>
        </div>
      </div>

      <div className={`msg-assistant-body relative transition-all duration-700 ${isStreaming ? "active-session shadow-[0_0_50px_rgba(251,191,36,0.05)]" : ""}`}>
        {isStreaming && (
          <div className="absolute -left-6 top-4 bottom-4 w-0.5 bg-gradient-to-b from-[var(--accent-gold)] via-[var(--accent-violet)] to-transparent opacity-30 animate-pulse" />
        )}
        <MessagePrimitive.Parts>
          {({ part }: { part: any }) => {
            const p = part as Record<string, any>;
            if (!p || !p.type) return null;
            const isStreaming = message.status?.type === "running";

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
              const isMessageDone = message.status?.type !== "running" && message.status?.type !== "requires-action";
              const isComplete = p.status?.type === "complete" || p.status?.type === "error" || isMessageDone;
              const hasResult = p.result !== undefined || isComplete;
              const args = p.args ?? {};
              const motionWrap = (el: React.ReactNode) => (
                <motion.div key={p.toolCallId} variants={toolCardVariants} initial="hidden" animate="visible">
                  {el}
                </motion.div>
              );

              switch (category) {
                case "terminal": {
                  const out = hasResult ? extractTerminalOutput(p.result) : {};
                  return motionWrap(
                    <TerminalTool
                      command={extractCommand(args)}
                      stdout={out.stdout}
                      stderr={out.stderr}
                      status={hasResult ? "complete" : "running"}
                    />
                  );
                }
                case "file-write":
                case "file-read":
                  return motionWrap(
                    <FileDiffTool
                      toolName={p.toolName}
                      filePath={extractFilePath(args)}
                      content={extractFileContent(args, p.result)}
                      status={hasResult ? "complete" : "running"}
                    />
                  );
                case "search":
                  return motionWrap(
                    <SearchTool
                      toolName={p.toolName}
                      query={extractSearchQuery(args)}
                      results={hasResult && Array.isArray(p.result) ? p.result : undefined}
                      status={hasResult ? "complete" : "running"}
                    />
                  );
                case "list":
                  return motionWrap(
                    <ListTool
                      toolName={p.toolName}
                      path={args?.path || args?.directory_path || args?.DirectoryPath}
                      items={hasResult && Array.isArray(p.result) ? p.result : undefined}
                      status={hasResult ? "complete" : "running"}
                    />
                  );
                case "task":
                  return motionWrap(
                    <TodoTool
                      todo={args?.todo || args?.todos || args?.text || args?.content}
                      status={hasResult ? "complete" : "running"}
                    />
                  );
                case "permission":
                  return motionWrap(
                    <PermissionCard
                      toolName={p.toolName}
                      args={args}
                      status={hasResult ? "complete" : "running"}
                    />
                  );
                default:
                  return motionWrap(
                    hasResult
                      ? <ToolResultBlock toolName={p.toolName} result={p.result} />
                      : <ToolCallBlock toolName={p.toolName} args={args} active={true} />
                  );
              }
            }
            return null;
          }}
        </MessagePrimitive.Parts>
        
        {/* Quantum Thinking — § Instant feedback without black hole effect */}
        {message.status?.type === "running" && isEffectivelyEmpty && (
          <div className="flex items-center gap-4 py-4 px-2 animate-fade-in">
             <div className="relative w-8 h-8 flex-shrink-0" style={{ perspective: '400px' }}>
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
                   Quantum Thinking
                </span>
                <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-wider">
                   Initializing Neural Circuitry...
                </span>
             </div>
          </div>
        )}

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
      className="group msg-user flex flex-col items-end mb-12"
      variants={messageVariants}
      initial="hidden"
      animate="visible"
    >
      <div className="msg-user-bubble bg-[var(--bg-elevated)] border border-[var(--border-default)] shadow-2xl relative overflow-hidden group-hover:border-[var(--border-bright)] transition-all duration-500">
        <div className="absolute inset-0 bg-gradient-to-br from-white/[0.02] to-transparent pointer-events-none" />
        <MessagePrimitive.Content />
      </div>

      <ActionBarPrimitive.Root className="flex items-center gap-4 mt-3 opacity-0 group-hover:opacity-100 transition-all duration-300 translate-y-1 group-hover:translate-y-0">
        <ActionBarPrimitive.Copy className="text-[10px] uppercase tracking-[0.2em] font-bold text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors cursor-pointer">
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
    <div className={`tool-call-block group relative overflow-hidden ${active ? "active-tool border-[var(--accent-gold)]/30" : "border-white/[0.05]"}`}>
      {active && (
        <div className="absolute inset-0 bg-gradient-to-r from-[var(--accent-gold)]/5 to-transparent animate-pulse-bloom" style={{ animationDuration: '3s' }} />
      )}
      <div className="tool-header flex items-center gap-3 relative z-10">
        <div className="relative w-5 h-5 flex items-center justify-center">
           {active ? (
             <>
               <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-20 blur-md rounded-full animate-pulse" />
               <svg className="absolute inset-[-2px] w-[calc(100%+4px)] h-[calc(100%+4px)]" style={{ animation: 'rotateOrbit 3s linear infinite' }}>
                 <circle cx="50%" cy="50%" r="8" fill="none" stroke="var(--accent-gold)" strokeWidth="1" strokeDasharray="1 4" opacity="0.4" />
                 <circle r="1.2" fill="var(--accent-gold)">
                   <animateMotion dur="1.5s" repeatCount="indefinite" path="M -8,0 a 8,8 0 1,0 16,0 a 8,8 0 1,0 -16,0" />
                 </circle>
               </svg>
               <div className="w-1 h-1 rounded-full bg-[var(--accent-gold)] animate-quantum-wobble" />
             </>
           ) : (
             <svg className="w-3.5 h-3.5 text-[var(--text-faint)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
               <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
             </svg>
           )}
        </div>
        <span className={`text-[10px] font-mono tracking-widest uppercase ${active ? "text-[var(--accent-gold)] font-bold" : "text-[var(--text-muted)]"}`}>
          {active ? "Quantum Executing" : "Task Completed"}: {toolName.toUpperCase()}
        </span>
      </div>
      
      {active && (
        <div className="mt-2 pl-8 flex flex-col gap-1.5">
           <div className="h-1 w-full bg-white/[0.02] rounded-full overflow-hidden">
              <motion.div 
                className="h-full bg-gradient-to-r from-[var(--accent-gold)] to-[var(--accent-violet)]"
                animate={{ x: ["-100%", "100%"] }}
                transition={{ repeat: Infinity, duration: 2, ease: "linear" }}
              />
           </div>
           <span className="text-[9px] font-mono text-[var(--text-faint)] uppercase tracking-tighter opacity-60">
              Processing Dimensional Data...
           </span>
        </div>
      )}
      <div className="p-3 pt-0 mt-3 font-mono text-[11px] text-[var(--text-muted)] opacity-70 break-all whitespace-pre-wrap border-t border-white/[0.03] pt-3">
        {Object.entries(args).map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`).join('\n')}
      </div>
    </div>
  );
}

/* ============================================================
   Tool Result Block — Collapsible with copy action
   ============================================================ */
function ToolResultBlock({ toolName, result }: { toolName: string; result: any }) {
  const [expanded, setExpanded] = useState(false);
  const { copy } = useCopyToClipboard();
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
          onClick={() => copy(resultStr)}
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
function Composer({ skills }: { skills?: string[] }) {
  const composingRef = useRef(false);
  const aui = useAui();
  const text = useAuiState((s) => s.composer.text);
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const [localText, setLocalText] = useState(text || "");
  const [menuOpen, setMenuOpen] = useState(false);

  // Sync global text changes (like clearing after send) back to local state
  React.useEffect(() => {
    if (!composingRef.current) {
      setLocalText(text || "");
      if (!text) setMenuOpen(false);
    }
  }, [text]);

  const handleCompositionStart = useCallback(() => {
    composingRef.current = true;
  }, []);

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
    
    if (!composingRef.current) {
      aui.composer().setText(val);
    }
  }, [aui]);

  const handleSelectCommand = useCallback((cmd: string) => {
    setLocalText(cmd);
    aui.composer().setText(cmd);
    setMenuOpen(false);
  }, [aui]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (menuOpen && (e.key === "ArrowUp" || e.key === "ArrowDown" || e.key === "Enter" || e.key === "Escape")) {
      e.preventDefault();
    }
  }, [menuOpen]);

  return (
    <ComposerPrimitive.Root className="composer-root">
      <div className="relative">
        <AnimatePresence>
          {menuOpen && (
            <CommandMenu
              key="command-menu"
              inputValue={localText}
              onSelect={handleSelectCommand}
              isOpen={menuOpen}
              onClose={() => setMenuOpen(false)}
              skills={skills}
            />
          )}
        </AnimatePresence>
        
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
            onKeyDown={handleKeyDown}
          />

          <div className="flex items-center gap-2">
            {isRunning && (
              <ComposerPrimitive.Cancel
                className="btn-icon text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.1)]"
                title="Stop"
              >
                <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                  <rect x="6" y="6" width="12" height="12" rx="2" />
                </svg>
              </ComposerPrimitive.Cancel>
            )}

            <ComposerPrimitive.Send className="btn-icon btn-primary">
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 12h14M12 5l7 7-7 7" />
              </svg>
            </ComposerPrimitive.Send>
          </div>
        </div>
      </div>
    </ComposerPrimitive.Root>
  );
}
