"use client";

import { motion } from "framer-motion";

interface ToolLoadingSkeletonProps {
  color?: string;
  label?: string;
}

export function ToolLoadingSkeleton({
  color = "var(--accent-emerald)",
  label = "Loading...",
}: ToolLoadingSkeletonProps) {
  return (
    <div className="bg-[#0c0c0f] px-4 py-4 flex items-center gap-3">
      <motion.div
        className="flex gap-1"
        animate={{ opacity: [0.3, 1, 0.3] }}
        transition={{ repeat: Infinity, duration: 1.2 }}
      >
        {[0, 1, 2].map((i) => (
          <span
            key={i}
            className="w-1.5 h-1.5 rounded-full opacity-60"
            style={{ backgroundColor: color }}
          />
        ))}
      </motion.div>
      <span className="text-[11px] font-mono text-[var(--text-faint)]">{label}</span>
    </div>
  );
}
