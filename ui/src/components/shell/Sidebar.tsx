"use client";

import Link from "next/link";
import { useRef } from "react";
import { usePathname } from "next/navigation";
import {
  Menu,
  Activity,
  CheckCircle2,
  ChevronsUpDown,
  Database,
  FileText,
  Grid2x2,
  Network,
  Settings,
  Shield,
  Sparkles,
  Server,
  UserRound,
} from "lucide-react";
import { useDashboardSummary } from "@/features/overview/queries";
import { useScope } from "@/features/scope/useScope";

const NAV_ITEMS = [
  { label: "Overview", href: "/", icon: Grid2x2 },
  { label: "Recommendations", href: "/recommendations", icon: Sparkles },
  { label: "Resources", href: "/resources", icon: Server },
  { label: "Policies", href: "/policies", icon: Shield },
  { label: "Plugins", href: "/plugins", icon: Network },
  { label: "Approvals", href: "/approvals", icon: CheckCircle2 },
  { label: "Audit", href: "/audit", icon: FileText },
  { label: "Reports", href: "/reports", icon: Activity },
] as const;

export function Sidebar() {
  const mobileDialog = useRef<HTMLDialogElement>(null);
  const pathname = usePathname();
  const { carry } = useScope();
  // Approvals badge = approvals assigned to the signed-in user, from the
  // same summary the Overview uses (shared cache, no extra request).
  const { data: summary } = useDashboardSummary({});
  const mine = summary?.pending_approvals_mine ?? 0;

  return (
    <>
    <button aria-label="Open navigation" className="md:hidden fixed top-3 left-3 z-50 rounded-btn border border-app-border bg-app-surface p-1 text-fg" onClick={() => mobileDialog.current?.showModal()}><Menu aria-hidden className="w-6 h-6" /></button>
    <dialog ref={mobileDialog} aria-label="Navigation" className="m-0 h-dvh max-h-dvh w-72 border-r border-app-border bg-app-sidebar p-4 text-fg backdrop:bg-black/50"><button className="mb-4 border border-app-border rounded-btn p-2 text-sm" onClick={() => mobileDialog.current?.close()}>Close navigation</button><nav aria-label="Primary" className="space-y-1">{NAV_ITEMS.map(item => <Link key={item.href} href={carry(item.href)} onClick={() => mobileDialog.current?.close()} className="block rounded-btn p-3 hover:bg-app-surface focus-visible:outline-2">{item.label}</Link>)}</nav></dialog>
    <aside className="hidden md:flex fixed left-0 top-0 h-dvh w-[var(--sidebar-width)] bg-app-sidebar border-r border-app-border z-50 flex-col select-none">
      <div className="h-16 px-4 flex items-center border-b border-app-border shrink-0">
        <div className="flex items-center gap-2.5">
          <div className="w-8 h-8 rounded-lg overflow-hidden bg-app-surface border border-app-border flex items-center justify-center p-0.5">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img alt="" className="w-full h-full object-cover rounded-md" src="/consize_dashboard_logo.png" />
          </div>
          <span className="font-semibold text-base tracking-tight text-fg">Consize</span>
        </div>
      </div>

      <div className="px-3 pt-4 pb-2 shrink-0">
        <span className="text-[11px] font-medium text-fg-muted px-2 uppercase tracking-wider">
          Operational queue
        </span>
      </div>

      {/* The nav scrolls on short viewports; the footer below never does. */}
      <nav aria-label="Primary" className="flex-1 min-h-0 px-3 space-y-1 overflow-y-auto">
        {NAV_ITEMS.map((item) => {
          const Icon = item.icon;
          const active = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
          return (
            <Link
              key={item.href}
              href={carry(item.href)}
              aria-current={active ? "page" : undefined}
              className={`group flex items-center justify-between gap-3 px-3 py-2 text-sm font-medium rounded-btn border-l-2 transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-500 ${
                active
                  ? "bg-ok-bg-strong text-ok-fg border-emerald-400"
                  : "border-transparent text-fg-secondary hover:text-fg hover:bg-app-surface"
              }`}
            >
              <span className="flex items-center gap-3 min-w-0">
                <Icon
                  className={`w-4 h-4 shrink-0 transition-colors ${active ? "text-ok-strong" : "text-fg-muted group-hover:text-fg-secondary"}`}
                  aria-hidden
                />
                <span className="truncate">{item.label}</span>
              </span>
              {item.label === "Approvals" && mine > 0 && (
                <span className="px-2 py-0.5 rounded-full bg-warn-bg text-warn-fg border border-warn-line text-xs font-semibold">
                  <span className="sr-only">{mine} assigned to you: </span>
                  {mine}
                </span>
              )}
            </Link>
          );
        })}
      </nav>

      {/* Pinned to the bottom of the viewport. Static placeholders for now;
          this becomes the workspace switcher / account menu when those
          features exist, so it deliberately has no interactive affordances. */}
      <div className="shrink-0 border-t border-app-border bg-app-sidebar p-3 space-y-2">
        <div className="flex items-center justify-between p-2 bg-app-surface border border-app-border rounded-btn">
          <div className="flex items-center gap-2 min-w-0">
            <div className="w-6 h-6 rounded bg-ok-bg border border-ok-line flex items-center justify-center shrink-0">
              <Database className="w-3.5 h-3.5 text-ok-strong" aria-hidden />
            </div>
            <div className="min-w-0">
              <div className="text-[10px] text-fg-muted uppercase tracking-wider leading-none mb-0.5">Workspace</div>
              <div className="text-xs font-medium text-fg truncate leading-tight">Current instance</div>
            </div>
          </div>
          <ChevronsUpDown className="w-3.5 h-3.5 text-fg-muted shrink-0" aria-hidden />
        </div>

        <div className="flex items-center justify-between px-1 pt-1">
          <div className="flex items-center gap-2 min-w-0">
            <div className="w-6 h-6 rounded-full bg-app-surface-alt border border-app-border flex items-center justify-center shrink-0 text-[11px] font-semibold text-fg-secondary">
              <UserRound className="w-3.5 h-3.5" aria-hidden />
            </div>
            <div className="min-w-0">
              <div className="text-xs font-medium text-fg truncate leading-tight">Identity unavailable</div>
              <div className="text-[10px] text-fg-muted font-medium leading-tight">Role unknown</div>
            </div>
          </div>
          <button
            type="button"
            disabled
            aria-label="Settings (not available yet)"
            className="p-1 text-fg-muted rounded disabled:cursor-not-allowed"
          >
            <Settings className="w-3.5 h-3.5" aria-hidden />
          </button>
        </div>
      </div>
    </aside>
    </>
  );
}