'use client';

import { useMemo } from 'react';
import type { UIMessage } from 'ai';
import MessageContent from './MessageContent';

interface MessageBubbleProps {
  message: UIMessage;
  isStreaming?: boolean;
}

export default function MessageBubble({ message, isStreaming }: MessageBubbleProps) {
  const isUser = message.role === 'user';

  const roleLabel = useMemo(() => (isUser ? 'You' : 'Assistant'), [isUser]);

  return (
    <div className={`flex animate-fade-in-up ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div className={`flex gap-3 max-w-[85%] ${isUser ? 'flex-row-reverse' : 'flex-row'}`}>
        {/* Avatar */}
        <div
          className={`flex-shrink-0 w-8 h-8 rounded-full flex items-center justify-center text-sm font-medium ${
            isUser ? 'bg-indigo-600 text-white' : 'bg-gray-200 text-gray-600'
          }`}
        >
          {isUser ? (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
            </svg>
          ) : (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
            </svg>
          )}
        </div>

        {/* Message bubble */}
        <div
          className={`rounded-2xl px-4 py-3 shadow-sm ${
            isUser
              ? 'bg-indigo-600 text-white rounded-br-md'
              : 'bg-white border border-gray-200 text-gray-800 rounded-bl-md'
          }`}
        >
          {/* Role label */}
          <div className={`text-xs font-medium mb-1 ${isUser ? 'text-indigo-200' : 'text-gray-500'}`}>
            {roleLabel}
          </div>

          {/* Content */}
          <MessageContent message={message} />

          {/* Streaming cursor */}
          {isStreaming && !isUser && (
            <span className={`inline-block ml-1 ${isUser ? 'text-indigo-200' : 'text-gray-400'} animate-blink`}>
              ▋
            </span>
          )}
        </div>
      </div>
    </div>
  );
}
