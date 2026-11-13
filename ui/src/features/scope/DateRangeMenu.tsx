"use client";

import { useCallback, useRef, useState } from "react";
import { Calendar, Check, ChevronDown } from "lucide-react";
import { useDismiss } from "@/lib/useDismiss";
import { DEFAULT_WINDOW, WINDOWS, useScope } from "./useScope";

export function DateRangeMenu() {
  const { range, update } = useScope();
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const close = useCallback(() => {
    setOpen(false);
    triggerRef.current?.focus();
  }, []);
  useDismiss(wrapRef, open, close);

  const current = WINDOWS.find((w) => w.value === range) ?? WINDOWS[2];

  return (
    <div ref={wrapRef} className="relative">
      <button
        ref={triggerRef}
        type="button"
        disabled
        title="Date filtering is not supported by the runtime API"
        aria-label="Date range unavailable"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 px-3 h-8 bg-app-surface border border-app-border hover:border-app-border-strong rounded-btn text-xs text-fg-secondary hover:text-fg transition-colors"
      >
        <Calendar className="w-3.5 h-3.5 text-fg-muted" aria-hidden />
        <span>Current state</span>
        <ChevronDown className="w-3.5 h-3.5 text-fg-muted" aria-hidden />
      </button>
      {open && (
        <div role="menu" aria-label="Date range" className="absolute right-0 top-10 z-50 w-44 bg-app-sidebar border border-app-border rounded-btn shadow-2xl p-1">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              type="button"
              role="menuitemradio"
              aria-checked={w.value === current.value}
              onClick={() => {
                update({ window: w.value === DEFAULT_WINDOW ? undefined : w.value });
                close();
              }}
              className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs text-fg-secondary hover:bg-app-hover hover:text-fg"
            >
              <span className="w-3.5">{w.value === current.value && <Check className="h-3.5 w-3.5 text-ok-strong" aria-hidden />}</span>
              {w.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}