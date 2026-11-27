"use client";

import { useSearchParams } from "next/navigation";
import { useUrlFilters } from "@/lib/useUrlFilters";
import { CatalogPanel } from "./CatalogPanel";
import { InstalledPanel } from "./InstalledPanel";
import { usePluginCatalog } from "./api";
import { installedOf } from "./catalogModel";

export type Tab = "installed" | "catalog";

export function PluginsPage() {
  const searchParams = useSearchParams();
  const { update } = useUrlFilters();
  const tab: Tab = searchParams.get("tab") === "catalog" ? "catalog" : "installed";
  const { data, isPending, isError, error, refetch } = usePluginCatalog();
  const installedCount = data ? installedOf(data.plugins).length : undefined;

  // Each tab has its own search, so switching clears the filters of the other.
  const select = (next: Tab) =>
    update({ tab: next === "installed" ? "" : next, q: "", capability: "", publisher: "", compatible: "" });

  const TAB = (on: boolean) =>
    "-mb-px cursor-pointer whitespace-nowrap border-b-2 px-0.5 py-3 text-xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 " +
    (on ? "border-emerald-400 font-semibold text-ok-strong" : "border-transparent font-medium text-fg-muted hover:text-fg");

  return (
    <>
      <header className="space-y-1.5">
        <h1 className="text-2xl font-semibold tracking-tight text-fg">Plugins &amp; Ecosystem</h1>
        <p className="text-sm text-fg-secondary">Discover and manage capabilities for your infrastructure.</p>
      </header>

      <div role="tablist" aria-label="Plugin sections" className="flex gap-7 border-b border-app-border">
        <button role="tab" id="tab-installed" aria-selected={tab === "installed"} aria-controls="plugins-panel" onClick={() => select("installed")} className={TAB(tab === "installed")}>
          Installed
          {installedCount !== undefined && <span className="font-num ml-2 text-fg-secondary">{installedCount}</span>}
        </button>
        <button role="tab" id="tab-catalog" aria-selected={tab === "catalog"} aria-controls="plugins-panel" onClick={() => select("catalog")} className={TAB(tab === "catalog")}>
          Catalog
        </button>
      </div>

      <section id="plugins-panel" role="tabpanel" aria-labelledby={tab === "installed" ? "tab-installed" : "tab-catalog"} className="pt-1">
        {isError && !data ? (
          <div role="alert" className="rounded-card border border-app-border bg-app-surface p-6">
            <p className="text-sm font-medium text-fg">Couldn&apos;t load plugins</p>
            <p className="mt-1 text-xs text-fg-muted">{error instanceof Error ? error.message : "The plugin service is unavailable."}</p>
            <button type="button" onClick={() => void refetch()} className="mt-3 h-8 cursor-pointer rounded-btn border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt">
              Retry
            </button>
          </div>
        ) : tab === "installed" ? (
          <InstalledPanel plugins={data?.plugins} loading={isPending} onBrowse={() => select("catalog")} />
        ) : (
          <CatalogPanel plugins={data?.plugins} loading={isPending} />
        )}
      </section>
    </>
  );
}