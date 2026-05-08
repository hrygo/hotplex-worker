'use client';

import { useEffect } from 'react';
import { BrandIcon } from '@/components/icons';

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error('[WebChat ErrorBoundary]', {
      message: error.message,
      digest: error.digest,
      stack: error.stack,
    });
  }, [error]);

  return (
    <div className="flex flex-col items-center justify-center h-screen bg-[var(--bg-base)]">
      <div className="w-16 h-16 rounded-2xl bg-[var(--bg-elevated)] flex items-center justify-center mb-6 border border-[var(--border-subtle)]">
        <BrandIcon size={48} className="opacity-40" />
      </div>
      <h2 className="text-lg font-display font-bold text-[var(--text-primary)] mb-2">
        Something went wrong
      </h2>
      <p className="text-sm text-[var(--text-muted)] mb-6 max-w-sm text-center">
        {error.message || 'An unexpected error occurred.'}
      </p>
      <button
        onClick={reset}
        className="px-6 py-2.5 rounded-full bg-[var(--accent-gold)] text-black text-sm font-bold transition-all hover:opacity-90 active:scale-[0.98]"
      >
        Try Again
      </button>
    </div>
  );
}
