"use client";

import { Bell, Search, UserRound } from "lucide-react";
import { DateRangeMenu } from "@/features/scope/DateRangeMenu";
import { ScopeMenu } from "@/features/scope/scopeMenu";
import { ThemeToggle } from "./ThemeToggle";

export function Topbar() {
  return (
    <header className="fixed top-0 left-[var(--sidebar-width)] right-0 h-14 bg-app-sidebar/95 backdrop-blur-md border-b border-app-border z-40 px-5 flex items-center justify-between gap-4">
      <div className="relative w-[min(34vw,420px)] min-w-[220px]">
        <Search className="w-4 h-4 text-fg-muted absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" aria-hidden />
        {/* No search index exists yet, so this stays disabled rather than
            looking live and doing nothing. */}
        <input
          type="text"
          disabled
          aria-label="Search resources, policies, and PRs (not available yet)"
          placeholder="Search resources, policies, and PRs..."
          className="w-full h-8 pl-9 pr-10 bg-app-field border border-app-border rounded-btn text-xs text-fg placeholder:text-fg-muted disabled:cursor-not-allowed"
        />
        <kbd className="absolute right-2.5 top-1/2 -translate-y-1/2 px-1.5 py-0.5 font-sans text-[10px] text-fg-muted bg-app-surface border border-app-border rounded">
          ⌘K
        </kbd>
      </div>

      <div className="flex items-center gap-2.5">
        <ScopeMenu />
        <DateRangeMenu />
        <ThemeToggle />
        {/* PRD §6.3: a badge must match real unread items. There is no
            notification source yet, so no dot is drawn. */}
        <button
          type="button"
          disabled
          aria-label="Notifications (none available yet)"
          className="relative w-8 h-8 flex items-center justify-center bg-app-surface border border-app-border rounded-btn text-fg-secondary disabled:cursor-not-allowed"
        >
          <Bell className="w-4 h-4" aria-hidden />
        </button>
        <div
          aria-hidden
          className="w-8 h-8 rounded-btn bg-ok-bg border border-ok-line flex items-center justify-center text-ok-fg font-medium text-xs"
        >
          <UserRound className="w-4 h-4" />
        </div>
      </div>
    </header>
  );
}