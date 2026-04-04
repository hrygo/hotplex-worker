'use client';

import { useMemo } from 'react';
import CopyButton from '../ui/CopyButton';

interface CodeBlockProps {
  children: string;
  className?: string;
  inline?: boolean;
}

export default function CodeBlock({ children, className, inline }: CodeBlockProps) {
  // Extract language from className like "language-typescript"
  const language = useMemo(() => {
    if (!className) return null;
    const match = className.match(/language-(\w+)/);
    return match ? match[1] : null;
  }, [className]);

  // Inline code (not inside a code block)
  if (inline) {
    return (
      <code className={`bg-gray-100 text-pink-600 px-1.5 py-0.5 rounded text-sm font-mono ${className || ''}`}>
        {children}
      </code>
    );
  }

  return (
    <div className="relative group rounded-lg overflow-hidden my-3">
      {/* Header with language label and copy button */}
      <div className="flex items-center justify-between bg-gray-800 px-4 py-2 text-xs">
        <span className="text-gray-400 font-mono uppercase tracking-wide">
          {language || 'code'}
        </span>
        <CopyButton text={children} className="opacity-0 group-hover:opacity-100 transition-opacity" />
      </div>
      {/* Code content */}
      <pre className="!mt-0 !rounded-t-none bg-gray-900 p-4 overflow-x-auto">
        <code className={`language-${language || 'text'}`}>
          {children}
        </code>
      </pre>
    </div>
  );
}
