import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'HotPlex Chat',
  description: 'AI-powered chat with HotPlex Worker Gateway',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
