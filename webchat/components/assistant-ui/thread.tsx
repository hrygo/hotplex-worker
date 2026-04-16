"use client";

import React from "react";
import {
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ActionBarPrimitive,
} from "@assistant-ui/react";
import { MarkdownText } from "./MarkdownText";
import { BrandIcon } from "@/components/icons";

/* ============================================================
   Thread — Main conversation wrapper
   ============================================================ */
export function Thread() {
  return (
    <ThreadPrimitive.Root className="flex flex-col h-full relative overflow-hidden noise">
      {/* Ambient warm glow */}
      <div
        className="absolute inset-0 pointer-events-none"
        style={{
          background:
            "radial-gradient(ellipse 50% 40% at 50% 0%, rgba(245,158,11,0.04) 0%, transparent 70%)",
        }}
        aria-hidden="true"
      />

      {/* Viewport */}
      <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto relative" style={{ zIndex: 1 }}>
        <div className="thread-content" style={{ paddingBottom: "2rem", paddingTop: "1.5rem" }}>
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
      </ThreadPrimitive.Viewport>

      {/* Scroll to bottom */}
      <ThreadPrimitive.ScrollToBottom className="scroll-btn-inner" title="Scroll to bottom">
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
        </svg>
      </ThreadPrimitive.ScrollToBottom>

      {/* Composer footer */}
      <ThreadPrimitive.ViewportFooter className="composer-footer relative" style={{ zIndex: 10 }}>
        <div className="thread-content">
          <Composer />
        </div>
      </ThreadPrimitive.ViewportFooter>
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
  { prompt: "解释系统架构设计", icon: "arch" },
] as const;

type SuggestionIcon = (typeof SUGGESTIONS)[number]["icon"];

const ICON_PATHS: Record<SuggestionIcon, string> = {
  code: "M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4",
  learn: "M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253",
  debug: "M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z",
  refactor: "M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15",
  arch: "M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4",
};

function WelcomeScreen() {
  return (
    <div className="welcome-screen">
      <div className="welcome-glow" aria-hidden="true" />

      <BrandIcon size={56} style={{ marginBottom: "1.5rem" }} />

      <h2 className="welcome-title">How can I help?</h2>
      <p className="welcome-subtitle">Ask me anything about code, debugging, or architecture.</p>

      <div className="suggestions-grid cascade">
        {SUGGESTIONS.map((s) => (
          <ThreadPrimitive.Suggestion key={s.prompt} prompt={s.prompt} send className="suggestion-card">
            <svg className="suggestion-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={ICON_PATHS[s.icon]} />
            </svg>
            {s.prompt}
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
  return (
    <MessagePrimitive.Root className="group msg-assistant animate-fade-in-up">
      <BrandIcon size={28} className="flex-shrink-0" style={{ marginTop: 2 }} />

      <div className="msg-assistant-body">
        <MessagePrimitive.Parts>
          {({ part }) => {
            if (part.type === "reasoning") {
              return <ReasoningBlock text={(part as { type: "reasoning"; text: string }).text} />;
            }
            if (part.type === "text") {
              return <MarkdownText text={(part as { type: "text"; text: string }).text} />;
            }
            return null;
          }}
        </MessagePrimitive.Parts>

        <ActionBarPrimitive.Root className="action-bar">
          <ActionBarPrimitive.Copy className="action-btn" />
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
    <MessagePrimitive.Root className="group msg-user">
      <div style={{ maxWidth: "80%", minWidth: 0 }}>
        <div className="msg-user-bubble">
          <MessagePrimitive.Content />
        </div>
        <ActionBarPrimitive.Root className="action-bar action-bar-user">
          <ActionBarPrimitive.Copy className="action-btn" />
          <ActionBarPrimitive.Edit className="action-btn">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
            </svg>
          </ActionBarPrimitive.Edit>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

/* ============================================================
   Reasoning Block
   ============================================================ */
function ReasoningBlock({ text }: { text: string }) {
  const [expanded, setExpanded] = React.useState(false);
  if (!text.trim()) return null;

  return (
    <div className="reasoning-block">
      <button onClick={() => setExpanded((v) => !v)} className="reasoning-toggle">
        <svg
          className="reasoning-chevron"
          style={{ transform: expanded ? "rotate(90deg)" : "rotate(0)" }}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span style={{ color: "var(--accent-amber)" }}>Reasoning</span>
        {text.length > 100 && !expanded && (
          <span className="reasoning-preview">{text.slice(0, 100)}...</span>
        )}
      </button>
      {expanded && <div className="reasoning-content animate-fade-in">{text}</div>}
    </div>
  );
}

/* ============================================================
   Composer
   ============================================================ */
function Composer() {
  return (
    <ComposerPrimitive.Root className="composer-root">
      <div className="composer-row">
        <ComposerPrimitive.Input className="focus-ring composer-input" rows={1} placeholder="Message HotPlex AI…" />

        <ComposerPrimitive.Cancel className="cancel-btn" title="Stop">
          <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <rect x="6" y="6" width="12" height="12" rx="2" />
          </svg>
        </ComposerPrimitive.Cancel>

        <ComposerPrimitive.Send className="send-btn" disabled>
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M12 5l7 7-7 7" />
          </svg>
        </ComposerPrimitive.Send>
      </div>
    </ComposerPrimitive.Root>
  );
}
