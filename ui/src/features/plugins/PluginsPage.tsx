"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ChevronDown, Plus, RefreshCw, Search, ShieldCheck } from "lucide-react";
import type { PluginCategory } from "@/schemas";
import { useScope } from "../scope/useScope";
import { PluginKpis } from "./PluginKpis";
import { PluginList } from "./PluginList";
import { PluginPanel } from "./PluginPanel";
import { usePlugins } from "./queries";

// `tab` and `pill` are deliberately two labels for the same filter: the
// approved design shows both rows. They bind to one URL param, so they can
// never disagree. (Worth asking design whether both rows are needed.)
const CATEGORIES: { key: PluginCategory; tab: string; pill: string }[] = [
  { key: "evidence", tab: "Evidence & data", pill: "Evidence" },
  { key: "algorithm", tab: "Algorithms", pill: "Algorithms" },
  { key: "action", tab: "Action providers", pill: "Actions" },
  { key: "notification", tab: "Notifications", pill: "Notifications" },
];

const HEADER_BTN =
  "inline-flex h-8 items-center gap-2 rounded-btn border border-app-border bg-app-surface px-3 text-xs font-medium text-fg-secondary opacity-60 cursor-not-allowed";

export function PluginsPage() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { update, isScoped } = useScope();

  const category = CATEGORIES.find((c) => c.key === searchParams.get("category"))?.key;
  const q = searchParams.get("q") ?? "";
  const pluginParam = searchParams.get("plugin") ?? undefined;

  const { data, isPending, isError, error, refetch } = usePlugins({ category, q: q || undefined });

  // Debounced search that writes to the URL without fighting the user's typing.
  const [text, setText] = useState(q);
  const lastPushed = useRef(q);
  useEffect(() => {
    if (q !== lastPushed.current) {
      lastPushed.current = q;
      setText(q);
    }
  }, [q]);
  useEffect(() => {
    if (text === lastPushed.current) return;
    const t = setTimeout(() => {
      lastPushed.current = text;
      update({ q: text || undefined });
    }, 250);
    return () => clearTimeout(t);
  }, [text, update]);

  const items = data?.items ?? [];
  // If the selection is filtered out or gone, fall back to the first visible
  // plugin rather than showing stale content (PRD §7.6).
  const selected = items.find((p) => p.id === pluginParam) ?? items[0];

  const hrefFor = (id: string) => {
    const p = new URLSearchParams(searchParams.toString());
    p.set("plugin", id);
    return `${pathname}?${p.toString()}`;
  };
  const counts = data?.summary.counts;
  const countOf = (c: PluginCategory) => counts?.[c];

  return (
    <>
      <section className="flex flex-wrap items-start justify-between gap-4 border-b border-app-border pb-4">
        <div>
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="text-[24px] font-semibold tracking-tight text-fg">Plugins &amp; Ecosystem</h1>
            {counts && (
              <span className="rounded border border-ok-line bg-ok-bg px-2 py-1 text-xs font-semibold text-ok-strong">
                {counts.installed} installed
              </span>
            )}
          </div>
          <p className="mt-1 text-sm text-fg-secondary">
            Manage telemetry, cost, recommendation, execution, and notification capabilities.
          </p>
          {isScoped && (
            // PRD §6.2: say so when a view isn't filterable by the active scope.
            <p className="mt-1 text-[11px] text-fg-muted">
              Plugins are workspace-wide, so the infrastructure scope doesn&apos;t apply here.
            </p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2 whitespace-nowrap">
          {/* Sync, verification and install are commands the backend doesn't
              expose yet (and the marketplace is a PRD §1.3 non-goal). */}
          <button type="button" disabled title="Not available yet" className={HEADER_BTN}>
            <RefreshCw className="h-4 w-4" aria-hidden />
            Sync all plugins
          </button>
          <button type="button" disabled title="Not available yet" className={HEADER_BTN}>
            <ShieldCheck className="h-4 w-4" aria-hidden />
            Verify credentials
          </button>
          <button type="button" disabled title="Not available yet" className={HEADER_BTN}>
            <Plus className="h-4 w-4" aria-hidden />
            Install plugin
          </button>
        </div>
      </section>

      {isError && !data ? (
        <div role="alert" className="rounded-card border border-app-border bg-app-surface p-6 shadow-card">
          <p className="text-sm font-medium text-fg">Couldn&apos;t load plugins</p>
          <p className="mt-1 text-xs text-fg-muted">
            {error instanceof Error ? error.message : "The plugin endpoint is unavailable."}
          </p>
          <button
            type="button"
            onClick={() => void refetch()}
            className="mt-3 h-8 rounded-btn border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt"
          >
            Retry
          </button>
        </div>
      ) : (
        <>
          {data ? (
            <PluginKpis summary={data.summary} />
          ) : (
            <div aria-label="Loading plugin summary" className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="h-32 animate-pulse rounded-card border border-app-border bg-app-surface" />
              ))}
            </div>
          )}

          <nav role="tablist" aria-label="Plugin sections" className="flex items-center gap-1 overflow-x-auto border-b border-app-border">
            <button
              role="tab"
              aria-selected={!category}
              aria-controls="plugin-panel"
              onClick={() => update({ category: undefined })}
              className={`whitespace-nowrap border-b-2 px-4 py-3 text-xs ${
                !category ? "border-emerald-400 font-semibold text-ok-strong" : "border-transparent font-medium text-fg-secondary hover:text-fg"
              }`}
            >
              Installed
              {counts && <span className={`ml-1.5 rounded-full px-1.5 ${!category ? "bg-ok-bg-strong" : "bg-app-surface-alt"}`}>{counts.installed}</span>}
            </button>
            {/* Marketplace is out of scope for this release (PRD §1.3). */}
            <button
              role="tab"
              aria-selected="false"
              disabled
              title="The plugin marketplace isn't available yet"
              className="whitespace-nowrap border-b-2 border-transparent px-4 py-3 text-xs font-medium text-fg-muted opacity-60 cursor-not-allowed"
            >
              Marketplace
            </button>
            {CATEGORIES.map((c) => {
              const on = category === c.key;
              return (
                <button
                  key={c.key}
                  role="tab"
                  aria-selected={on}
                  aria-controls="plugin-panel"
                  onClick={() => update({ category: c.key })}
                  className={`whitespace-nowrap border-b-2 px-4 py-3 text-xs ${
                    on ? "border-emerald-400 font-semibold text-ok-strong" : "border-transparent font-medium text-fg-secondary hover:text-fg"
                  }`}
                >
                  {c.tab}
                  {counts && <span className={`ml-1.5 rounded-full px-1.5 ${on ? "bg-ok-bg-strong" : "bg-app-surface-alt"}`}>{countOf(c.key)}</span>}
                </button>
              );
            })}
          </nav>

          <section id="plugin-panel" role="tabpanel" className="space-y-4">
            <div className="rounded-card border border-app-border bg-app-surface p-3 shadow-card">
              <div className="flex flex-wrap items-center gap-2">
                <div className="relative min-w-[260px] flex-1">
                  <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-muted" aria-hidden />
                  <input
                    type="search"
                    value={text}
                    onChange={(e) => setText(e.target.value)}
                    aria-label="Search installed plugins"
                    placeholder="Search installed plugins…"
                    className="h-9 w-full rounded-btn border border-app-border bg-app-field pl-9 pr-3 text-xs text-fg placeholder:text-fg-muted focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500/40"
                  />
                </div>
                <div role="group" aria-label="Filter by plugin type" className="inline-flex rounded-btn border border-app-border bg-app-inset p-1 text-xs">
                  <button
                    type="button"
                    aria-pressed={!category}
                    onClick={() => update({ category: undefined })}
                    className={`rounded-md px-3 py-1.5 ${!category ? "bg-ok-bg-strong font-medium text-ok-strong" : "text-fg-secondary hover:text-fg"}`}
                  >
                    All{counts ? ` (${counts.installed})` : ""}
                  </button>
                  {CATEGORIES.map((c) => (
                    <button
                      key={c.key}
                      type="button"
                      aria-pressed={category === c.key}
                      onClick={() => update({ category: c.key })}
                      className={`rounded-md px-3 py-1.5 ${category === c.key ? "bg-ok-bg-strong font-medium text-ok-strong" : "text-fg-secondary hover:text-fg"}`}
                    >
                      {c.pill}
                      {counts ? ` (${countOf(c.key)})` : ""}
                    </button>
                  ))}
                </div>
                {/* One workspace exists today, so there is nothing to choose. */}
                <span className="relative">
                  <select
                    disabled
                    aria-label="Workspace"
                    title="Only one workspace exists"
                    className="h-9 min-w-[160px] appearance-none rounded-btn border border-app-border bg-app-field pl-3 pr-8 text-xs text-fg-secondary opacity-80 cursor-not-allowed"
                  >
                    <option>All workspaces</option>
                  </select>
                  <ChevronDown className="pointer-events-none absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-fg-muted" aria-hidden />
                </span>
              </div>
            </div>

            <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-12">
              <div className="min-w-0 xl:col-span-8">
                {isPending ? (
                  <div aria-label="Loading plugins" className="space-y-3">
                    {Array.from({ length: 4 }).map((_, i) => (
                      <div key={i} className="h-40 animate-pulse rounded-card border border-app-border bg-app-surface" />
                    ))}
                  </div>
                ) : items.length === 0 ? (
                  <div className="rounded-card border border-app-border bg-app-surface px-4 py-10 text-center shadow-card">
                    <p className="text-sm font-medium text-fg">
                      {category || q ? "No plugins match these filters" : "No plugins installed"}
                    </p>
                    <p className="mt-1 text-xs text-fg-muted">
                      {category || q
                        ? "Clear the search or choose another type."
                        : "Built-in Kubernetes and Prometheus plugins ship with Consize and appear here once enabled."}
                    </p>
                    {(category || q) && (
                      <Link href={pathname} className="mt-3 inline-flex h-8 items-center rounded-btn border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt">
                        Reset filters
                      </Link>
                    )}
                  </div>
                ) : (
                  <PluginList
                    items={items}
                    selectedId={selected?.id}
                    hrefFor={hrefFor}
                    onSelect={(id) => router.push(hrefFor(id))}
                  />
                )}
              </div>
              <div className="xl:sticky xl:top-20 xl:col-span-4">
                <PluginPanel id={selected?.id} />
              </div>
            </div>
          </section>
        </>
      )}
    </>
  );
}