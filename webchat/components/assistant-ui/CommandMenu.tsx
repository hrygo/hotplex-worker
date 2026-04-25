"use client";

import React, { useState, useEffect, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";

interface Command {
  key: string;
  label: string;
  description: string;
  type: "slash" | "skill";
}

const SLASH_COMMANDS: Command[] = [
  { key: "/gc", label: "/gc", description: "Trigger garbage collection and session cleanup", type: "slash" },
  { key: "/reset", label: "/reset", description: "Reset current session and clear history", type: "slash" },
  { key: "/park", label: "/park", description: "Park the current session to save resources", type: "slash" },
  { key: "/new", label: "/new", description: "Create a fresh new session", type: "slash" },
  { key: "/status", label: "/status", description: "Show current session and worker status", type: "slash" },
  { key: "/cd", label: "/cd", description: "Switch working directory and create new session", type: "slash" },
  { key: "/skills", label: "/skills", description: "List currently loaded skills and their usage", type: "slash" },
  { key: "/help", label: "/help", description: "Show available commands and documentation", type: "slash" },
];

interface CommandMenuProps {
  inputValue: string;
  onSelect: (value: string) => void;
  isOpen: boolean;
  onClose: () => void;
  skills?: string[];
}

export function CommandMenu({ inputValue, onSelect, isOpen, onClose, skills }: CommandMenuProps) {
  const [selectedIndex, setSelectedIndex] = useState(0);

  const COMMANDS: Command[] = useMemo(() => [
    ...SLASH_COMMANDS,
    ...(skills ?? []).map(name => ({
      key: name,
      label: name,
      description: `${name} skill`,
      type: "skill" as const,
    })),
  ], [skills]);

  // Filter commands based on input value
  // If starts with /, filter only slash commands. Otherwise filter skills.
  const isSlash = inputValue.startsWith("/");
  const filterText = isSlash ? inputValue.slice(1).toLowerCase() : inputValue.toLowerCase();

  const filtered = COMMANDS.filter(cmd => {
    if (isSlash) {
      if (cmd.type !== "slash") return false;
      if (!filterText) return true; // Show all slash commands if only '/' is typed
      return cmd.key.toLowerCase().includes(inputValue.toLowerCase()) ||
             cmd.description.toLowerCase().includes(filterText);
    }

    // Skill filtering
    if (!inputValue) return false;
    if (cmd.type !== "skill") return false;
    return cmd.key.toLowerCase().includes(filterText) ||
           cmd.description.toLowerCase().includes(filterText);
  }).slice(0, 8);

  useEffect(() => {
    setSelectedIndex(0);
  }, [inputValue]);

  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIndex(prev => (prev + 1) % filtered.length);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIndex(prev => (prev - 1 + filtered.length) % filtered.length);
      } else if (e.key === "Enter" && filtered.length > 0) {
        e.preventDefault();
        onSelect(filtered[selectedIndex].key);
      } else if (e.key === "Escape") {
        onClose();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, filtered, selectedIndex, onSelect, onClose]);

  if (!isOpen || filtered.length === 0) return null;

  return (
    <div
      style={{ zIndex: 99999, pointerEvents: 'auto' }}
      className="absolute bottom-full left-0 right-0 mb-2 rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-[var(--border-default)] shadow-[0_12px_48px_rgba(0,0,0,0.6)] backdrop-blur-2xl overflow-hidden"
    >
      <div className="px-3 py-2 text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest border-b border-[var(--border-subtle)] flex justify-between items-center bg-[rgba(255,255,255,0.02)]">
        <span>{isSlash ? "System Commands" : "Available Skills"}</span>
        <span className="opacity-50">↑↓ to navigate · Enter to select</span>
      </div>

      <div className="max-h-[320px] overflow-y-auto custom-scrollbar">
        {filtered.map((cmd, i) => (
          <button
            key={cmd.key}
            className={`w-full px-4 py-3 text-left flex flex-col gap-0.5 transition-all ${
              i === selectedIndex
                ? "bg-[var(--bg-hover)] translate-x-1"
                : "hover:bg-[rgba(255,255,255,0.02)]"
            }`}
            onClick={() => onSelect(cmd.key)}
            onMouseEnter={() => setSelectedIndex(i)}
          >
            <div className="flex items-center gap-2">
              <span className={`text-xs font-bold ${i === selectedIndex ? "text-[var(--accent-gold)]" : "text-[var(--text-primary)]"}`}>
                {cmd.label}
              </span>
              {cmd.type === "slash" ? (
                <span className="text-[9px] px-1.5 py-0.5 rounded bg-[rgba(251,191,36,0.1)] text-[var(--accent-gold)] font-mono font-bold uppercase">CMD</span>
              ) : (
                <span className="text-[9px] px-1.5 py-0.5 rounded bg-[rgba(52,211,153,0.1)] text-[var(--accent-emerald)] font-mono font-bold uppercase">SKILL</span>
              )}
            </div>
            <p className="text-[11px] text-[var(--text-muted)] line-clamp-1 italic">
              {cmd.description}
            </p>
          </button>
        ))}
      </div>
    </div>
  );
}
