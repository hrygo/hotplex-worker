'use client';

import dynamic from 'next/dynamic';
import { BrandIcon } from '@/components/icons';

const ChatUI = dynamic(() => import('./components/chat/ChatContainer.assistant-ui'), {
  ssr: false,
  loading: () => (
    <div className="flex flex-col h-screen" style={{ background: 'var(--bg-base)' }}>
      <header className="app-header">
        <div style={{ maxWidth: "46rem", margin: "0 auto", padding: "0.75rem 1.5rem" }}>
          <div className="flex items-center gap-3">
            <BrandIcon size={36} />
            <div>
              <h1 className="text-base font-semibold" style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}>
                HotPlex AI
              </h1>
              <p className="text-sm" style={{ color: 'var(--text-faint)', fontFamily: 'var(--font-mono)', fontSize: '0.6875rem' }}>
                Loading…
              </p>
            </div>
          </div>
        </div>
      </header>
      <div className="flex-1 flex items-center justify-center">
        <div className="flex items-center gap-3" style={{ color: 'var(--text-faint)' }}>
          <svg className="w-5 h-5 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
          </svg>
          <span style={{ fontFamily: 'var(--font-body)', fontSize: '0.875rem' }}>Initializing…</span>
        </div>
      </div>
    </div>
  ),
});

export default function Page() {
  return <ChatUI />;
}
