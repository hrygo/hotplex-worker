'use client';

import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';
import type { UIMessage } from 'ai';
import CodeBlock from './CodeBlock';

interface MessageContentProps {
  message: UIMessage;
}

function getTextContent(message: UIMessage): string {
  return message.parts
      .filter((part): part is { type: 'text'; text: string } => part.type === 'text')
      .map((part) => part.text)
      .join('');
}

export default function MessageContent({ message }: MessageContentProps) {
  const text = getTextContent(message);
  
  // User messages: plain text
  if (message.role === 'user') {
    return <p className="whitespace-pre-wrap break-words">{text}</p>;
  }

  return (
    <div className="prose prose-sm max-w-none">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          // Custom code block rendering
          pre: ({ children }) => <>{children}</>,
          code: ({ className, children, ...props }) => {
            const isInline = !className;
            return (
              <CodeBlock className={className} inline={isInline}>
                {String(children).replace(/\n$/, '')}
              </CodeBlock>
            );
          },
          // External links open in new tab
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:underline">
              {children}
            </a>
          ),
          // Tables responsive
          table: ({ children }) => (
            <div className="overflow-x-auto my-2">
              <table className="min-w-full divide-y divide-gray-200 text-sm">{children}</table>
            </div>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
