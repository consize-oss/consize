"use client";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ChevronRight, Plus } from "lucide-react";
import { useUrlFilters } from "@/lib/useUrlFilters";
import { PluginLogo } from "./PluginLogo";
import { SearchField } from "./SearchField";
import { filterInstalled, installedOf } from "./catalogModel";
import type { Plugin } from "./schemas";
import { CAPABILITY_LABELS } from "./schemas";
import { INSTALLATION_STATUS, TONE_TEXT } from "./status";

const HEADINGS = ["Plugin", "Capability", "Status", "Declared access"];

export function InstalledPanel({
  plugins,
  loading,
  onBrowse,
}: {
  plugins: Plugin[] | undefined;
  loading: boolean;
  onBrowse: () => void;
}) {
  const router = useRouter();
  const q = useSearchParams().get("q") ?? "";
  const { update } = useUrlFilters();
  const rows = plugins ? filterInstalled(plugins, q) : [];
  const anyInstalled = plugins ? installedOf(plugins).length > 0 : false;

  return (
    <div className="space-y-5 pt-4">
      <div className="flex items-center gap-4">
        <SearchField value={q} onChange={(value) => update({ q: value })} placeholder="Search installed plugins…" label="Search installed plugins" />
        <button
          type="button"
          onClick={onBrowse}
          className="inline-flex h-10 cursor-pointer items-center gap-2 whitespace-nowrap rounded-btn border border-app-border bg-app-surface px-4 text-xs font-medium text-fg-secondary hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
        >
          <Plus aria-hidden="true" className="h-4 w-4" />
          Add plugin
        </button>
      </div>

      {loading ? (
        <div role="status" aria-label="Loading installed plugins" className="space-y-px overflow-hidden rounded-card border border-app-border">
          {Array.from({ length: 3 }, (_, i) => (
            <div key={i} aria-hidden="true" className="h-[72px] animate-pulse bg-app-surface-alt motion-reduce:animate-none" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div role="status" className="rounded-card border border-app-border bg-app-surface px-4 py-12 text-center">
          <p className="text-sm font-medium text-fg">{anyInstalled ? "No installed plugins match your search" : "No plugins installed yet"}</p>
          <p className="mt-1 text-xs text-fg-muted">
            {anyInstalled ? "Try a different search." : "Browse the catalog to add the capabilities you need."}
          </p>
          {!anyInstalled && (
            <button type="button" onClick={onBrowse} className="mt-4 h-8 cursor-pointer rounded-btn border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt">
              Browse the catalog
            </button>
          )}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-card border border-app-border bg-app-surface shadow-card">
          <table className="w-full min-w-[760px] border-collapse text-left">
            <caption className="sr-only">Installed plugins, their capability, status and declared access.</caption>
            <thead className="text-[10px] uppercase tracking-wide text-fg-muted">
              <tr className="border-b border-app-border">
                {HEADINGS.map((h) => (
                  <th key={h} scope="col" className="px-5 py-3.5 font-medium">{h}</th>
                ))}
                <th scope="col" className="w-12"><span className="sr-only">Open</span></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-app-border text-xs">
              {rows.map((p) => {
                const status = INSTALLATION_STATUS[p.installation!.status];
                return (
                  <tr
                    key={p.id}
                    onClick={() => router.push(`/plugins/${encodeURIComponent(p.id)}`)}
                    className="cursor-pointer transition-colors hover:bg-app-surface-alt/50"
                  >
                    <th scope="row" className="px-5 py-5 text-left font-normal">
                      <div className="flex items-center gap-3">
                        <PluginLogo brand={p.brand} label={p.name} />
                        <div className="min-w-0">
                          <Link
                            href={`/plugins/${encodeURIComponent(p.id)}`}
                            onClick={(e) => e.stopPropagation()}
                            className="text-[13px] font-medium text-fg hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
                          >
                            {p.name}
                          </Link>
                          <p className="mt-1 text-[11px] text-fg-muted">{p.version} · {p.package_label}</p>
                        </div>
                      </div>
                    </th>
                    <td className="px-5 py-5 text-fg">{p.capabilities.map((c) => CAPABILITY_LABELS[c]).join(", ")}</td>
                    <td className={"px-5 py-5 font-medium " + TONE_TEXT[status.tone]}>
                      {status.label}
                      {p.installation!.status_reason && <span className="block text-[11px] font-normal text-fg-muted">{p.installation!.status_reason}</span>}
                    </td>
                    <td className="px-5 py-5 text-fg">{p.installation!.declared_access}</td>
                    <td className="px-5 py-5 text-right text-fg-muted">
                      <ChevronRight aria-hidden="true" className="ml-auto h-4 w-4" />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}