import React from "react";

const LIGHTBULB_PATH =
  "M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z";

export function BrandIcon({ size = 28, style, className }: { size?: number; style?: React.CSSProperties; className?: string }) {
  return (
    <div className={`brand-icon ${className ?? ""}`} style={{ width: size, height: size, ...style }}>
      <svg
        style={{ width: size * 0.5, height: size * 0.5, color: "var(--accent-amber)" }}
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={LIGHTBULB_PATH} />
      </svg>
    </div>
  );
}

export const WORKER_DISPLAY: Record<string, string> = {
  claude_code: "claude",
  opencode_cli: "opencode",
  opencode_server: "opencode",
  acpx: "acpx",
  pimon: "pimon",
};
