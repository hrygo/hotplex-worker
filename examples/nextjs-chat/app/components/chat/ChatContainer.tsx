'use client';

import { useChat } from '@ai-sdk/react';
import { DefaultChatTransport } from 'ai';
import { useState } from 'react';
import MessageList from './MessageList';
import ChatInput from './ChatInput';
import ThinkingIndicator from './ThinkingIndicator';
import ErrorMessage from './ErrorMessage';

export default function ChatContainer() {
  const [input, setInput] = useState('');
  
  const { messages, status, error, stop, regenerate, sendMessage } = useChat({
    transport: new DefaultChatTransport({ api: '/api/chat' }),
  });

  const isLoading = status === 'streaming' || status === 'submitted';

  const handleSend = (message: string) => {
    sendMessage({ text: message });
  };

  return (
    <div className="flex flex-col h-screen bg-gray-50">
      <header className="bg-white border-b px-4 py-3 flex-shrink-0">
        <div className="max-w-3xl mx-auto">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center">
              <svg className="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
              </svg>
            </div>
            <div>
              <h1 className="text-lg font-semibold text-gray-900">HotPlex AI</h1>
              <p className="text-sm text-gray-500">AI SDK • AEP v1 Protocol</p>
            </div>
          </div>
        </div>
      </header>

      {status === 'submitted' && (
        <div className="flex justify-start px-4 py-2">
          <ThinkingIndicator />
        </div>
      )}

      <MessageList messages={messages} status={status} />

      {error && <ErrorMessage error={error} onRetry={regenerate} />}

      <ChatInput
        input={input}
        onInputChange={setInput}
        onSend={handleSend}
        onStop={stop}
        isLoading={isLoading}
      />
    </div>
  );
}
