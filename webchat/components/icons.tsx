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
