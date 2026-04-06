"use client";

import {
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ActionBarPrimitive,
  ChainOfThoughtPrimitive,
  useAui,
  useAuiState,
} from "@assistant-ui/react";
import { MarkdownText } from "./MarkdownText";

// SuggestionState shape
interface SuggestionState {
  title: string;
  label: string;
  prompt: string;
}

/* ============================================================
   SuggestionItem — Dark glassmorphic welcome cards
   ============================================================ */
function SuggestionItem({
  suggestion,
  delay,
}: {
  suggestion: SuggestionState;
  delay: number;
}) {
  const aui = useAui();
  const isRunning = useAuiState((s) => s.thread.isRunning);

  const handleClick = () => {
    if (isRunning) return;
    aui.composer().setText(suggestion.prompt);
  };

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={isRunning}
      className="group w-full text-left rounded-2xl border px-5 py-4 transition-all duration-200 cursor-pointer
        disabled:opacity-40 disabled:cursor-not-allowed
        animate-cascade"
      style={{
        animationDelay: `${delay}ms`,
        background: "rgba(12, 21, 41, 0.6)",
        borderColor: "rgba(51, 65, 85, 0.3)",
        backdropFilter: "blur(12px)",
        boxShadow: "0 4px 20px rgba(0,0,0,0.4)",
      }}
      onMouseEnter={(e) => {
        if (!isRunning) {
          e.currentTarget.style.borderColor = "rgba(16, 185, 129, 0.4)";
          e.currentTarget.style.boxShadow = "0 0 24px rgba(16,185,129,0.15), 0 8px 32px rgba(0,0,0,0.5)";
          e.currentTarget.style.background = "rgba(16, 185, 129, 0.05)";
        }
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.borderColor = "rgba(51, 65, 85, 0.3)";
        e.currentTarget.style.boxShadow = "0 4px 20px rgba(0,0,0,0.4)";
        e.currentTarget.style.background = "rgba(12, 21, 41, 0.6)";
      }}
    >
      <div className="flex items-start gap-4">
        {/* Icon */}
        <div
          className="flex-shrink-0 w-10 h-10 rounded-xl flex items-center justify-center transition-all duration-200"
          style={{
            background: "linear-gradient(135deg, rgba(16,185,129,0.15) 0%, rgba(6,182,212,0.1) 100%)",
            border: "1px solid rgba(16,185,129,0.2)",
            boxShadow: "0 0 12px rgba(16,185,129,0.1) inset",
          }}
        >
          <svg
            className="w-5 h-5"
            style={{ color: "var(--accent-emerald)" }}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={1.5}
              d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"
            />
          </svg>
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0">
          {/* Label */}
          <div className="flex items-center gap-2 mb-1.5">
            <span
              className="text-[10px] font-medium px-2 py-0.5 rounded-full uppercase tracking-wider"
              style={{
                background: "rgba(16,185,129,0.12)",
                color: "var(--accent-emerald)",
                fontFamily: "var(--font-mono)",
                letterSpacing: "0.08em",
              }}
            >
              {suggestion.label}
            </span>
          </div>
          <p
            className="text-sm font-medium line-clamp-2 transition-colors duration-200"
            style={{
              color: "var(--text-primary)",
              fontFamily: "var(--font-display)",
              lineHeight: 1.5,
            }}
          >
            {suggestion.title}
          </p>
          <p
            className="text-xs mt-1 line-clamp-1"
            style={{
              color: "var(--text-muted)",
              fontFamily: "var(--font-mono)",
            }}
          >
            {suggestion.prompt}
          </p>
        </div>

        {/* Arrow indicator */}
        <div
          className="flex-shrink-0 w-6 h-6 rounded-lg flex items-center justify-center opacity-0 group-hover:opacity-100 transition-all duration-200 mt-0.5"
          style={{
            background: "rgba(16,185,129,0.15)",
            color: "var(--accent-emerald)",
          }}
        >
          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
        </div>
      </div>
    </button>
  );
}

/* ============================================================
   ReasoningPart — Inside ChainOfThought accordion
   ============================================================ */
function ReasoningPart({ text }: { text: string }) {
  if (!text) return null;
  return (
    <div
      className="text-sm leading-relaxed"
      style={{
        color: "var(--text-secondary)",
        fontFamily: "var(--font-mono)",
        lineHeight: 1.8,
        opacity: 0.85,
      }}
    >
      {text}
    </div>
  );
}

/* ============================================================
   ChainOfThoughtWrapper — Accordion for thinking display
   ============================================================ */
function ChainOfThoughtWrapper() {
  return (
    <ChainOfThoughtPrimitive.Root>
      <ChainOfThoughtPrimitive.AccordionTrigger
        className="flex items-center gap-2 text-xs transition-colors duration-150 cursor-pointer mb-3 mt-1 rounded-lg px-2 py-1"
        style={{
          color: "var(--accent-cyan)",
          fontFamily: "var(--font-mono)",
          letterSpacing: "0.02em",
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.background = "rgba(6,182,212,0.08)";
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.background = "transparent";
        }}
      >
        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={1.5}
            d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"
          />
        </svg>
        <span>思考过程</span>
        <svg
          className="w-3 h-3 transition-transform duration-200 ml-1"
          style={{}}
          data-slot="chevron"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </ChainOfThoughtPrimitive.AccordionTrigger>

      <div
        className="pl-4 border-l-2 rounded-sm mb-3"
        style={{
          borderColor: "rgba(6,182,212,0.25)",
          background: "rgba(6,182,212,0.04)",
          paddingLeft: "1rem",
          paddingTop: "0.75rem",
          paddingBottom: "0.75rem",
          borderRadius: "0 6px 6px 0",
        }}
      >
        <ChainOfThoughtPrimitive.Parts components={{ Text: ReasoningPart }} />
      </div>
    </ChainOfThoughtPrimitive.Root>
  );
}

/* ============================================================
   UserMessage — Glassmorphic user bubble
   ============================================================ */
function UserMessage() {
  return (
    <MessagePrimitive.Root className="group flex justify-end">
      <div
        className="max-w-xl animate-fade-in-up rounded-2xl rounded-br-md px-4 py-3"
        style={{
          background: "rgba(16,185,129,0.12)",
          border: "1px solid rgba(16,185,129,0.2)",
          boxShadow: "0 2px 12px rgba(0,0,0,0.3)",
        }}
      >
        <div style={{ color: "var(--text-primary)", fontFamily: "var(--font-display)" }}>
          <MessagePrimitive.Parts />
        </div>

        {/* Action bar */}
        <ActionBarPrimitive.Root
          autohide="not-last"
          autohideFloat="always"
          className="flex items-center gap-1 mt-1.5 justify-end"
          style={{ opacity: 0 }}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "1")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "0")}
        >
          <ActionBarPrimitive.Edit
            className="p-1.5 rounded-lg transition-all duration-150 cursor-pointer"
            style={{
              color: "var(--text-muted)",
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = "rgba(51,65,85,0.4)";
              e.currentTarget.style.color = "var(--text-secondary)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = "transparent";
              e.currentTarget.style.color = "var(--text-muted)";
            }}
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
              />
            </svg>
          </ActionBarPrimitive.Edit>
          <ActionBarPrimitive.Copy
            className="p-1.5 rounded-lg transition-all duration-150 cursor-pointer group/copy"
            style={{ color: "var(--text-muted)" }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = "rgba(51,65,85,0.4)";
              e.currentTarget.style.color = "var(--text-secondary)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = "transparent";
              e.currentTarget.style.color = "var(--text-muted)";
            }}
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
              />
            </svg>
          </ActionBarPrimitive.Copy>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

/* ============================================================
   AssistantMessage — Dark glass AI response card
   ============================================================ */
function AssistantMessage() {
  return (
    <MessagePrimitive.Root className="group flex justify-start animate-fade-in-up">
      <div
        className="max-w-2xl rounded-2xl rounded-bl-md px-5 py-4"
        style={{
          background: "rgba(12, 21, 41, 0.75)",
          border: "1px solid rgba(51, 65, 85, 0.35)",
          backdropFilter: "blur(16px)",
          boxShadow: "0 4px 24px rgba(0,0,0,0.5), inset 0 1px 0 rgba(255,255,255,0.03)",
        }}
      >
        {/* Avatar + name row */}
        <div className="flex items-center gap-2.5 mb-3 pb-2.5" style={{ borderBottom: "1px solid rgba(51,65,85,0.2)" }}>
          <div
            className="w-8 h-8 rounded-xl flex items-center justify-center flex-shrink-0"
            style={{
              background: "linear-gradient(135deg, rgba(16,185,129,0.25) 0%, rgba(6,182,212,0.2) 100%)",
              border: "1px solid rgba(16,185,129,0.25)",
              boxShadow: "0 0 12px rgba(16,185,129,0.15)",
            }}
          >
            <svg className="w-4 h-4" style={{ color: "var(--accent-emerald)" }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"
              />
            </svg>
          </div>
          <div>
            <span
              className="text-sm font-semibold"
              style={{ color: "var(--text-primary)", fontFamily: "var(--font-display)" }}
            >
              HotPlex AI
            </span>
            <span
              className="ml-2 text-[10px] px-1.5 py-0.5 rounded-full font-mono"
              style={{
                background: "rgba(16,185,129,0.1)",
                color: "var(--accent-emerald)",
                border: "1px solid rgba(16,185,129,0.15)",
                letterSpacing: "0.05em",
              }}
            >
              AEP v1
            </span>
          </div>
        </div>

        {/* Message content */}
        <div style={{ color: "var(--text-secondary)", fontFamily: "var(--font-display)", lineHeight: 1.75 }}>
          <MessagePrimitive.Parts
            components={
              {
                Text: MarkdownText,
                ChainOfThought: ChainOfThoughtWrapper,
              } as const
            }
          />
        </div>

        {/* Action bar */}
        <ActionBarPrimitive.Root
          autohide="not-last"
          autohideFloat="always"
          className="flex items-center gap-1 mt-3 pt-2.5"
          style={{
            borderTop: "1px solid rgba(51,65,85,0.15)",
            opacity: 0,
          }}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "1")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "0")}
        >
          <ActionBarPrimitive.Copy
            className="p-1.5 rounded-lg transition-all duration-150 cursor-pointer group/copy"
            style={{ color: "var(--text-muted)" }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = "rgba(51,65,85,0.4)";
              e.currentTarget.style.color = "var(--text-secondary)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = "transparent";
              e.currentTarget.style.color = "var(--text-muted)";
            }}
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
              />
            </svg>
          </ActionBarPrimitive.Copy>
          <ActionBarPrimitive.Reload
            className="p-1.5 rounded-lg transition-all duration-150 cursor-pointer"
            style={{ color: "var(--text-muted)" }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = "rgba(51,65,85,0.4)";
              e.currentTarget.style.color = "var(--text-secondary)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = "transparent";
              e.currentTarget.style.color = "var(--text-muted)";
            }}
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={1.5}
                d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
              />
            </svg>
          </ActionBarPrimitive.Reload>
          <ActionBarPrimitive.FeedbackPositive
            className="p-1.5 rounded-lg transition-all duration-150 cursor-pointer text-sm"
            style={{ color: "var(--text-muted)" }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = "rgba(51,65,85,0.4)";
              e.currentTarget.style.color = "var(--accent-emerald)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = "transparent";
              e.currentTarget.style.color = "var(--text-muted)";
            }}
          >
            👍
          </ActionBarPrimitive.FeedbackPositive>
          <ActionBarPrimitive.FeedbackNegative
            className="p-1.5 rounded-lg transition-all duration-150 cursor-pointer text-sm"
            style={{ color: "var(--text-muted)" }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = "rgba(51,65,85,0.4)";
              e.currentTarget.style.color = "#f87171";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = "transparent";
              e.currentTarget.style.color = "var(--text-muted)";
            }}
          >
            👎
          </ActionBarPrimitive.FeedbackNegative>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

/* ============================================================
   Composer — Glassmorphic input with emerald glow
   ============================================================ */
function Composer() {
  const isRunning = useAuiState((s) => s.thread.isRunning);

  return (
    <ComposerPrimitive.Root className="flex items-end gap-3">
      {/* Input */}
      <ComposerPrimitive.Input
        className="flex-1 w-full px-4 py-3.5 resize-none transition-all duration-200 rounded-2xl"
        rows={1}
        placeholder="输入消息，Shift+Enter 换行..."
        style={{
          background: "rgba(12, 21, 41, 0.7)",
          border: "1px solid rgba(51, 65, 85, 0.4)",
          color: "var(--text-primary)",
          fontFamily: "var(--font-display)",
          fontSize: "0.9rem",
          lineHeight: 1.6,
          backdropFilter: "blur(12px)",
          outline: "none",
          caretColor: "var(--accent-emerald)",
        }}
        onFocus={(e) => {
          e.currentTarget.style.borderColor = "rgba(16,185,129,0.45)";
          e.currentTarget.style.boxShadow = "0 0 0 3px rgba(16,185,129,0.08), 0 0 20px rgba(16,185,129,0.1)";
        }}
        onBlur={(e) => {
          e.currentTarget.style.borderColor = "rgba(51, 65, 85, 0.4)";
          e.currentTarget.style.boxShadow = "none";
        }}
      />

      {/* Send / Cancel */}
      {isRunning ? (
        <ComposerPrimitive.Cancel
          className="flex-shrink-0 w-11 h-11 flex items-center justify-center rounded-xl transition-all duration-150 cursor-pointer"
          style={{
            background: "rgba(239, 68, 68, 0.15)",
            border: "1px solid rgba(239, 68, 68, 0.3)",
            color: "#f87171",
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.background = "rgba(239, 68, 68, 0.25)";
            e.currentTarget.style.boxShadow = "0 0 16px rgba(239,68,68,0.2)";
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = "rgba(239, 68, 68, 0.15)";
            e.currentTarget.style.boxShadow = "none";
          }}
        >
          <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <rect x="6" y="6" width="12" height="12" rx="2" />
          </svg>
        </ComposerPrimitive.Cancel>
      ) : (
        <ComposerPrimitive.Send
          className="flex-shrink-0 w-11 h-11 flex items-center justify-center rounded-xl transition-all duration-150 cursor-pointer"
          style={{
            background: "linear-gradient(135deg, rgba(16,185,129,0.85) 0%, rgba(6,182,212,0.7) 100%)",
            border: "1px solid rgba(16,185,129,0.4)",
            color: "#fff",
            boxShadow: "0 0 16px rgba(16,185,129,0.25)",
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.background = "linear-gradient(135deg, rgba(16,185,129,1) 0%, rgba(6,182,212,0.9) 100%)";
            e.currentTarget.style.boxShadow = "0 0 24px rgba(16,185,129,0.4)";
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = "linear-gradient(135deg, rgba(16,185,129,0.85) 0%, rgba(6,182,212,0.7) 100%)";
            e.currentTarget.style.boxShadow = "0 0 16px rgba(16,185,129,0.25)";
          }}
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"
            />
          </svg>
        </ComposerPrimitive.Send>
      )}
    </ComposerPrimitive.Root>
  );
}

/* ============================================================
   Thread — Main conversation wrapper
   ============================================================ */
export function Thread() {
  const suggestions: SuggestionState[] = [
    {
      title: "帮我写一个 React 组件",
      label: "代码",
      prompt: "帮我写一个 React 组件",
    },
    {
      title: "解释这段代码的逻辑",
      label: "学习",
      prompt: "解释这段代码的逻辑",
    },
    {
      title: "帮我调试这个错误",
      label: "调试",
      prompt: "帮我调试这个错误",
    },
    {
      title: "重构这段代码让它更简洁",
      label: "重构",
      prompt: "重构这段代码让它更简洁",
    },
    {
      title: "解释系统架构设计",
      label: "架构",
      prompt: "解释系统架构设计",
    },
  ];

  return (
    <ThreadPrimitive.Root
      className="flex flex-col h-full relative overflow-hidden"
    >
      {/* Atmospheric background layer */}
      <div
        className="absolute inset-0 pointer-events-none"
        style={{
          background: "linear-gradient(180deg, #030712 0%, #060d1f 50%, #030712 100%)",
        }}
        aria-hidden="true"
      >
        {/* Grid dot pattern */}
        <div
          className="absolute inset-0 grid-pattern opacity-40"
          style={{ opacity: 0.35 }}
        />

        {/* Ambient orbs */}
        <div
          className="ambient-orb animate-orb-float"
          style={{
            width: "600px",
            height: "600px",
            top: "-15%",
            right: "-10%",
            background: "radial-gradient(circle, rgba(16,185,129,0.06) 0%, transparent 70%)",
          }}
        />
        <div
          className="ambient-orb"
          style={{
            width: "500px",
            height: "500px",
            bottom: "-20%",
            left: "-15%",
            background: "radial-gradient(circle, rgba(6,182,212,0.05) 0%, transparent 70%)",
            animationDelay: "-6s",
            animationDuration: "22s",
          }}
        />
      </div>

      {/* Viewport — scrollable message area */}
      <ThreadPrimitive.Viewport
        className="flex-1 overflow-y-auto relative"
        style={{ zIndex: 1 }}
      >
        <div
          className="space-y-5 max-w-3xl mx-auto px-4 py-6"
          style={{ paddingBottom: "2rem" }}
        >
          {/* Welcome suggestions */}
          <ThreadPrimitive.Suggestions>
            {({ suggestion }: { suggestion: SuggestionState }) => {
              const idx = suggestions.findIndex((s) => s.prompt === suggestion.prompt);
              return (
                <SuggestionItem
                  suggestion={suggestion}
                  delay={idx * 80}
                />
              );
            }}
          </ThreadPrimitive.Suggestions>

          {/* Messages */}
          <ThreadPrimitive.Messages>
            {({ message }) => {
              if (message.role === "user") {
                return <UserMessage />;
              }
              return <AssistantMessage />;
            }}
          </ThreadPrimitive.Messages>
        </div>
      </ThreadPrimitive.Viewport>

      {/* Scroll to bottom button */}
      <ThreadPrimitive.ScrollToBottom
        className="absolute flex items-center justify-center w-10 h-10 rounded-xl transition-all duration-200 cursor-pointer"
        style={{
          bottom: "90px",
          right: "24px",
          zIndex: 10,
          background: "rgba(12, 21, 41, 0.85)",
          border: "1px solid rgba(51, 65, 85, 0.4)",
          backdropFilter: "blur(12px)",
          color: "var(--text-secondary)",
          boxShadow: "0 4px 16px rgba(0,0,0,0.4)",
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.borderColor = "rgba(16,185,129,0.4)";
          e.currentTarget.style.color = "var(--accent-emerald)";
          e.currentTarget.style.boxShadow = "0 0 16px rgba(16,185,129,0.15), 0 4px 16px rgba(0,0,0,0.4)";
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.borderColor = "rgba(51, 65, 85, 0.4)";
          e.currentTarget.style.color = "var(--text-secondary)";
          e.currentTarget.style.boxShadow = "0 4px 16px rgba(0,0,0,0.4)";
        }}
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M19 14l-7 7m0 0l-7-7m7 7V3"
          />
        </svg>
      </ThreadPrimitive.ScrollToBottom>

      {/* Composer footer — glassmorphic bar */}
      <ThreadPrimitive.ViewportFooter
        className="relative px-4 py-3"
        style={{
          zIndex: 10,
          background: "rgba(6, 10, 22, 0.85)",
          borderTop: "1px solid rgba(51, 65, 85, 0.25)",
          backdropFilter: "blur(20px)",
          boxShadow: "0 -8px 32px rgba(0,0,0,0.3)",
        }}
      >
        {/* Top glow line */}
        <div
          aria-hidden="true"
          style={{
            position: "absolute",
            top: 0,
            left: "50%",
            transform: "translateX(-50%)",
            width: "60%",
            height: "1px",
            background: "linear-gradient(90deg, transparent, rgba(16,185,129,0.3), transparent)",
          }}
        />
        <div className="max-w-3xl mx-auto">
          <Composer />
        </div>
      </ThreadPrimitive.ViewportFooter>
    </ThreadPrimitive.Root>
  );
}
