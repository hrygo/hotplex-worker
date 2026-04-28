import type { Metadata } from 'next';
import { NuqsAdapter } from 'nuqs/adapters/next/app';
import { Inter, Outfit, JetBrains_Mono } from 'next/font/google';
import './globals.css';

const inter = Inter({
  subsets: ['latin'],
  display: 'swap',
  variable: '--font-inter',
});

const outfit = Outfit({
  subsets: ['latin'],
  display: 'swap',
  variable: '--font-outfit',
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  display: 'swap',
  variable: '--font-jetbrains',
});

export const metadata: Metadata = {
  title: 'HotPlex AI',
  description: 'AI-powered coding agent — HotPlex Worker Gateway',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" className={`dark ${inter.variable} ${outfit.variable} ${jetbrainsMono.variable}`}>
      <head />
      <body>
        <NuqsAdapter>{children}</NuqsAdapter>
      </body>
    </html>
  );
}
