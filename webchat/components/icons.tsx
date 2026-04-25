import React from "react";

export function BrandIcon({ size = 28, style, className }: { size?: number; style?: React.CSSProperties; className?: string }) {
  return (
    <div className={`brand-icon flex items-center justify-center ${className ?? ""}`} style={{ width: size, height: size, ...style }}>
      <img 
        src="/logo.webp" 
        alt="HotPlex Logo" 
        style={{ width: "100%", height: "100%", objectFit: "contain" }}
      />
    </div>
  );
}

export const WORKER_DISPLAY: Record<string, string> = {
  claude_code: "claude",
  opencode_server: "opencode",
  acpx: "acpx",
  pimon: "pimon",
};

export function WorkerIcon({ type, className }: { type: string; className?: string }) {
  switch (type) {
    case 'claude_code':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5" />
        </svg>
      );
    case 'opencode_server':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="16 18 22 12 16 6" />
          <polyline points="8 6 2 12 8 18" />
        </svg>
      );
    case 'acpx':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="3" y="11" width="18" height="10" rx="2" />
          <circle cx="12" cy="5" r="2" />
          <path d="M12 7v4" />
          <line x1="8" y1="16" x2="8" y2="16" />
          <line x1="16" y1="16" x2="16" y2="16" />
        </svg>
      );
    case 'pimon':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M4 7h16" />
          <path d="M4 12h16" />
          <path d="M4 17h16" />
          <path d="M7 7v10" />
          <path d="M17 7v10" />
        </svg>
      );
    default:
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="16" x2="12" y2="12" />
          <line x1="12" y1="8" x2="12.01" y2="8" />
        </svg>
      );
  }
}
