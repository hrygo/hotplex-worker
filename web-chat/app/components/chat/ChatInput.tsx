'use client';

import { useState, useRef, useCallback, KeyboardEvent, ChangeEvent } from 'react';

interface ChatInputProps {
  input: string;
  onInputChange: (value: string) => void;
  onSend: (message: string) => void;
  onStop: () => void;
  isLoading: boolean;
}

export default function ChatInput({ input, onInputChange, onSend, onStop, isLoading }: ChatInputProps) {
  const [isFocused, setIsFocused] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const adjustHeight = useCallback(() => {
    const textarea = textareaRef.current;
    if (textarea) {
      textarea.style.height = 'auto';
      textarea.style.height = `${Math.min(textarea.scrollHeight, 150)}px`;
    }
  }, []);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        if (!isLoading && input.trim()) {
          onSend(input.trim());
          onInputChange('');
          if (textareaRef.current) {
            textareaRef.current.style.height = 'auto';
          }
        }
      }
      if (e.key === 'Escape' && isLoading) {
        e.preventDefault();
        onStop();
      }
    },
    [isLoading, input, onSend, onStop]
  );

  const handleChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    onInputChange(e.target.value);
    adjustHeight();
  };

  return (
    <div className={`border-t p-4 transition-colors ${isFocused ? 'bg-gray-50' : 'bg-white'}`}>
      <div className="flex items-end gap-3 max-w-3xl mx-auto">
        <div className="flex-1 relative">
          <textarea
            id="chat-input"
            ref={textareaRef}
            value={input}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            onFocus={() => setIsFocused(true)}
            onBlur={() => setIsFocused(false)}
            placeholder="Ask me anything..."
            disabled={isLoading}
            rows={1}
            aria-label="Chat input"
            name="chat-input"
            className={`w-full px-4 py-3 border rounded-xl resize-none transition-all duration-150 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:bg-gray-100 disabled:cursor-not-allowed ${
              isFocused ? 'shadow-sm' : 'shadow'
            }`}
            style={{ minHeight: '48px', maxHeight: '150px' }}
          />
        </div>

        {isLoading ? (
          <button
            type="button"
            onClick={onStop}
            aria-label="Stop generating"
            className="flex-shrink-0 p-3 bg-red-500 text-white rounded-xl hover:bg-red-600 transition-colors shadow"
          >
            <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
              <rect x="6" y="6" width="12" height="12" rx="2" />
            </svg>
          </button>
        ) : (
          <button
            type="button"
            onClick={() => {
              if (input.trim()) {
                onSend(input.trim());
                onInputChange('');
                if (textareaRef.current) {
                  textareaRef.current.style.height = 'auto';
                }
              }
            }}
            disabled={!input.trim()}
            aria-label="Send message"
            className={`flex-shrink-0 p-3 rounded-xl font-medium transition-all duration-150 ${
              input.trim()
                ? 'bg-indigo-600 text-white hover:bg-indigo-700 shadow-md hover:shadow-lg'
                : 'bg-gray-200 text-gray-400 cursor-not-allowed'
            }`}
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
            </svg>
          </button>
        )}
      </div>

      <div className="flex justify-center mt-2">
        <p className="text-xs text-gray-400">
          Press <kbd className="px-1 py-0.5 bg-gray-100 rounded text-gray-500">Enter</kbd> to send,{' '}
          <kbd className="px-1 py-0.5 bg-gray-100 rounded text-gray-500">Shift + Enter</kbd> for new line,{' '}
          <kbd className="px-1 py-0.5 bg-gray-100 rounded text-gray-500">Esc</kbd> to stop
        </p>
      </div>
    </div>
  );
}
