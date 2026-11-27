"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { ChevronDown } from "lucide-react";
import { useUrlFilters } from "@/lib/useUrlFilters";
import { PluginLogo } from "./PluginLogo";
import { SearchField } from "./SearchField";
import { filterCatalog, publishersOf, type CatalogFilters } from "./catalogModel";
import { CAPABILITIES, CAPABILITY_LABELS, capabilitySchema, type Plugin } from "./schemas";
import { cardState, TONE_TEXT } from "./status";

export function CatalogPanel({ plugins, loading }: { plugins: Plugin[] | undefined; loading: boolean }) {
  const params = useSearchParams();
  const { update } = useUrlFilters();

  const capability = capabilitySchema.safeParse(params.get("capability"));
  const filters: CatalogFilters = {
    q: params.get("q") ?? "",
    capability: capability.success ? capability.data : "",
    publisher: params.get("publisher") ?? "",
    compatibleOnly: params.get("compatible") === "1",
  };
  const hasFilters = Boolean(filters.q || filters.capability || filters.publisher || filters.compatibleOnly);
  const shown = plugins ? filterCatalog(plugins, filters) : [];
  const publishers = plugins ? publishersOf(plugins) : [];

  const RAIL = (on: boolean) =>
    "flex w-full cursor-pointer items-center justify-between rounded-md px-3 py-2.5 text-left text-xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 " +
    (on ? "bg-ok-bg font-medium text-ok-strong" : "text-fg-secondary hover:bg-app-surface-alt");

  return (
    <div className="grid grid-cols-1 gap-6 pt-5 lg:grid-cols-[175px_minmax(0,1fr)]">
      <aside className="space-y-6">
        <nav aria-label="Capability">
          <p className="px-3 pb-2 text-[10px] uppercase tracking-[0.1em] text-fg-muted">Capability</p>
          <ul>
            <li>
              <button type="button" aria-pressed={!filters.capability} onClick={() => update({ capability: "" })} className={RAIL(!filters.capability)}>
                All plugins
                {plugins && <span className="font-num">{plugins.length}</span>}
              </button>
            </li>
            {CAPABILITIES.map((c) => (
              <li key={c}>
                <button type="button" aria-pressed={filters.capability === c} onClick={() => update({ capability: c })} className={RAIL(filters.capability === c)}>
                  {CAPABILITY_LABELS[c]}
                </button>
              </li>
            ))}
          </ul>
        </nav>

        <div>
          <label htmlFor="plugin-publisher" className="block px-3 pb-2 text-[10px] uppercase tracking-[0.1em] text-fg-muted">
            Publisher
          </label>
          <div className="relative">
            <select
              id="plugin-publisher"
              value={filters.publisher}
              onChange={(e) => update({ publisher: e.target.value })}
              className="h-10 w-full cursor-pointer appearance-none rounded-md border border-app-border bg-app-bg pl-3 pr-8 text-xs text-fg-secondary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
            >
              <option value="">All publishers</option>
              {publishers.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
            <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-muted" />
          </div>
        </div>
      </aside>

      <div className="min-w-0">
        <div className="flex items-center gap-5">
          <SearchField value={filters.q} onChange={(value) => update({ q: value })} placeholder="Search plugins…" label="Search plugins" />
          <label className="flex cursor-pointer items-center gap-2.5 whitespace-nowrap text-xs text-fg-secondary">
            <input
              type="checkbox"
              checked={filters.compatibleOnly}
              onChange={(e) => update({ compatible: e.target.checked ? "1" : "" })}
              className="h-3.5 w-3.5 cursor-pointer accent-emerald-600"
            />
            Compatible only
          </label>
        </div>

        <p role="status" aria-live="polite" className="mb-4 mt-4 text-[11px] text-fg-muted">
          {loading ? "Loading plugins…" : `${shown.length} ${shown.length === 1 ? "plugin" : "plugins"}`}
        </p>

        {loading ? (
          <div aria-hidden="true" className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {Array.from({ length: 4 }, (_, i) => (
              <div key={i} className="h-44 animate-pulse rounded-card border border-app-border bg-app-surface-alt motion-reduce:animate-none" />
            ))}
          </div>
        ) : shown.length === 0 ? (
          <div className="rounded-card border border-app-border bg-app-surface px-4 py-12 text-center">
            <p className="text-sm font-medium text-fg">{hasFilters ? "No plugins match these filters" : "The catalog is empty"}</p>
            <p className="mt-1 text-xs text-fg-muted">{hasFilters ? "Clear a filter or search for something else." : "No plugins are available to install yet."}</p>
            {hasFilters && (
              <button
                type="button"
                onClick={() => update({ q: "", capability: "", publisher: "", compatible: "" })}
                className="mt-4 h-8 cursor-pointer rounded-btn border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt"
              >
                Reset filters
              </button>
            )}
          </div>
        ) : (
          <ul className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {shown.map((p) => {
              const state = cardState(p);
              return (
                <li key={p.id}>
                  <Link
                    href={`/plugins/${encodeURIComponent(p.id)}`}
                    className="flex h-full min-h-[164px] flex-col gap-3 rounded-card border border-app-border bg-app-surface p-5 shadow-card transition-colors hover:border-app-border-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
                  >
                    <div className="flex items-center justify-between gap-3">
                      <PluginLogo brand={p.brand} label={p.name} />
                      <span className="text-[10px] text-fg-muted">{p.publisher.name}</span>
                    </div>
                    <h2 className="text-sm font-semibold text-fg">{p.name}</h2>
                    <p className="text-xs leading-relaxed text-fg-secondary">{p.description}</p>
                    <div className="mt-auto flex items-center justify-between gap-3 text-[11px]">
                      <span className="text-fg-muted">{p.capabilities.map((c) => CAPABILITY_LABELS[c]).join(" · ")}</span>
                      <span className={state.tone === "neutral" ? "text-fg-muted" : "font-medium " + TONE_TEXT[state.tone]}>{state.label}</span>
                    </div>
                  </Link>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}