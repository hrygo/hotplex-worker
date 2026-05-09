'use client';

import React from 'react';

interface Props {
  children: React.ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

/**
 * Catches assistant-ui internal MessageRepository errors (e.g. "same id already
 * exists in the parent tree" — github.com/assistant-ui/assistant-ui/issues/2380)
 * and recovers by remounting the chat interface with a fresh adapter state.
 */
export class AdapterErrorBoundary extends React.Component<Props, State> {
  state: State = { hasError: false, error: null };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidUpdate(prevProps: Props, prevState: State) {
    if (prevState.hasError && !this.state.hasError) {
      // Recovered — no action needed
    }
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null });
  };

  render() {
    if (this.state.hasError) {
      const isRepoError =
        this.state.error?.message?.includes('MessageRepository') ??
        false;

      if (isRepoError) {
        return (
          <div className="flex flex-col items-center justify-center h-full gap-4 p-8 text-center">
            <p className="text-sm text-[var(--text-muted)]">
              Chat state encountered an internal error. Click below to reload.
            </p>
            <button
              onClick={this.handleRetry}
              className="px-6 py-2 rounded-full bg-[var(--accent-gold)] text-black text-sm font-bold hover:scale-105 active:scale-95 transition-all"
            >
              Reload Chat
            </button>
          </div>
        );
      }

      // Non-repository errors — re-throw for higher-level error boundaries
      throw this.state.error;
    }

    return this.props.children;
  }
}
