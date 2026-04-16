'use client';

import dynamic from 'next/dynamic';
import { BrandIcon } from '@/components/icons';

const ChatUI = dynamic(() => import('./components/chat/ChatContainer.assistant-ui'), {
  ssr: false,
  loading: () => (
    <div className="flex flex-col h-screen bg-[var(--bg-base)]">
      <header className="app-header">
        <div className="header-inner">
          <div className="flex items-center gap-3">
            <BrandIcon size={36} />
            <div>
              <h1 className="header-title">HotPlex AI</h1>
              <p className="header-subtitle">Loading...</p>
            </div>
          </div>
        </div>
      </header>
      <div className="flex-1 flex items-center justify-center">
        <div className="flex flex-col items-center gap-4 text-[var(--text-muted)]">
          <div className="relative">
            <BrandIcon size={48} />
            <div className="absolute inset-0 animate-ping rounded-full bg-[var(--amber-light)] opacity-0" style={{ animationDuration: '2s' }} />
          </div>
          <div className="flex items-center gap-3">
            <svg className="w-5 h-5 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
            </svg>
            <span className="text-sm">Initializing assistant...</span>
          </div>
        </div>
      </div>
    </div>
  ),
});

export default function Page() {
  return <ChatUI />;
}
