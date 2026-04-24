"use client";

import React, { useState } from "react";
import {
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ActionBarPrimitive,
  useMessage,
} from "@assistant-ui/react";
import { MarkdownText } from "./MarkdownText";
import { BrandIcon } from "@/components/icons";

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

        {/* Scroll to bottom button — only shown when needed */}
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
    <div className="flex flex-col items-center justify-center py-24 text-center animate-fade-in-up">
      <div className="relative mb-8 flex items-center justify-center">
        <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-10 blur-3xl rounded-full scale-[2]" />
        
        {/* Orbital rings */}
        <div className="absolute w-32 h-32 border border-[var(--border-gold)] opacity-20 rounded-full animate-[spin_10s_linear_infinite]" />
        <div className="absolute w-40 h-40 border border-[var(--accent-emerald)] opacity-10 rounded-full animate-[spin_15s_linear_infinite_reverse]" />
        
        {/* Orbiting particles */}
        <div className="absolute w-full h-full animate-[orbit_4s_linear_infinite]">
           <div className="w-2 h-2 rounded-full bg-[var(--accent-gold)] blur-[1px]" />
        </div>
        
        <BrandIcon size={84} className="relative z-10 animate-float" />
      </div>

      <h1 className="text-4xl font-display font-bold tracking-tight mb-3 text-gradient-gold">
        How can I help you today?
      </h1>
      <p className="text-lg text-[var(--text-secondary)] mb-12 max-w-lg mx-auto">
        Ask me anything about code, debugging, or software architecture. 
        Empowered by HotPlex Intelligence.
      </p>

      <div className="grid grid-cols-2 gap-3 w-full max-w-xl mx-auto">
        {SUGGESTIONS.map((s) => (
          <ThreadPrimitive.Suggestion 
            key={s.prompt} 
            prompt={s.prompt} 
            send 
            className="suggestion-card flex items-center gap-3"
          >
            <div className="w-8 h-8 rounded-lg bg-[var(--bg-elevated)] flex items-center justify-center text-[var(--accent-gold)]">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={ICON_PATHS[s.icon]} />
              </svg>
            </div>
            <span className="font-medium">{s.prompt}</span>
          </ThreadPrimitive.Suggestion>
        ))}
      </div>
    </div>
  );
}

/* ============================================================
   Assistant Message
   ============================================================ */
function AssistantMessage() {
  const message = useMessage();
  
  return (
    <MessagePrimitive.Root className="group msg-assistant animate-fade-in-up">
      <div className="flex-shrink-0 mt-1">
        <div className="w-9 h-9 rounded-xl glass-dark flex items-center justify-center border border-[var(--border-default)]">
          <BrandIcon size={24} />
        </div>
      </div>

      <div className="msg-assistant-body">
        <MessagePrimitive.Parts>
          {({ part }) => {
            const m = message as any;
            const p = part as any;
            const isLatest = m.status.type === "running";
            
            if (p.type === "reasoning") {
              return <ReasoningBlock text={p.text} />;
            }
            if (p.type === "text") {
              return <MarkdownText text={p.text} />;
            }
            if (p.type === "tool-call") {
              return <ToolCallBlock 
                key={p.toolCallId} 
                toolName={p.toolName} 
                args={p.args} 
                active={isLatest && !m.content.some((other: any) => other.toolCallId === p.toolCallId && other.type === 'tool-result')} 
              />;
            }
            if (p.type === "tool-result") {
              return <ToolResultBlock key={p.toolCallId} toolName={p.toolName} result={p.result} />;
            }
            return null;
          }}
        </MessagePrimitive.Parts>

        <ActionBarPrimitive.Root className="flex items-center gap-2 mt-4 opacity-0 group-hover:opacity-100 transition-opacity">
          <ActionBarPrimitive.Copy className="text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors px-2 py-1 rounded hover:bg-[var(--bg-elevated)]" />
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

/* ============================================================
   User Message
   ============================================================ */
function UserMessage() {
  return (
    <MessagePrimitive.Root className="group msg-user animate-fade-in-up">
      <div className="msg-user-bubble">
        <MessagePrimitive.Content />
      </div>
      
      <ActionBarPrimitive.Root className="flex items-center gap-3 mt-2 opacity-0 group-hover:opacity-100 transition-opacity">
        <ActionBarPrimitive.Copy className="text-[10px] uppercase tracking-wider font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors">
          Copy
        </ActionBarPrimitive.Copy>
      </ActionBarPrimitive.Root>
    </MessagePrimitive.Root>
  );
}

/* ============================================================
   Reasoning Block
   ============================================================ */
function ReasoningBlock({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  if (!text.trim()) return null;

  return (
    <div className="reasoning-block">
      <div className="reasoning-header" onClick={() => setExpanded(!expanded)}>
        <svg
          className={`w-3.5 h-3.5 transition-transform ${expanded ? "rotate-90" : ""}`}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span>THOUGHT</span>
        {!expanded && text.length > 60 && (
          <span className="opacity-40 ml-2 truncate font-normal">{text.slice(0, 60)}...</span>
        )}
      </div>
      {expanded && (
        <div className="p-3 pt-0 text-[var(--text-muted)] text-sm whitespace-pre-wrap animate-fade-in font-mono leading-relaxed">
          {text}
        </div>
      )}
    </div>
  );
}

/* ============================================================
   Tool Call Block
   ============================================================ */
function ToolCallBlock({ toolName, args, active }: { toolName: string; args: any, active?: boolean }) {
  return (
    <div className={`tool-call-block ${active ? 'animate-pulse-subtle border-[var(--accent-emerald)]' : ''}`}>
      <div className="tool-header">
        <svg className={`w-3.5 h-3.5 ${active ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
        </svg>
        <span className={active ? 'font-bold' : ''}>
          {active ? 'EXECUTING' : 'CALLED'}: {toolName.toUpperCase()}
        </span>
      </div>
      <div className="p-3 pt-0 font-mono text-[11px] text-[var(--text-muted)] opacity-80">
        {JSON.stringify(args, null, 2)}
      </div>
    </div>
  );
}

/* ============================================================
   Tool Result Block
   ============================================================ */
function ToolResultBlock({ toolName, result }: { toolName: string; result: any }) {
  const [expanded, setExpanded] = useState(false);
  const resultStr = typeof result === 'string' ? result : JSON.stringify(result, null, 2);

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
          <svg
            className={`ml-auto w-3.5 h-3.5 transition-transform ${expanded ? "rotate-90" : ""}`}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
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

      {expanded && (
        <div className="p-3 pt-0 font-mono text-[11px] text-[var(--text-secondary)] whitespace-pre-wrap animate-fade-in border-t border-[var(--border-subtle)] mt-1">
          {resultStr}
        </div>
      )}
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
          <ComposerPrimitive.Cancel className="btn-icon text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.1)]" title="Stop">
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

