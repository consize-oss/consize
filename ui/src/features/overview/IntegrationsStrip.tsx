"use client";

import Link from "next/link";
import { usePluginCatalog } from "../plugins/api";
import { installedOf } from "../plugins/catalogModel";
import { CAPABILITY_LABELS } from "../plugins/schemas";
import { INSTALLATION_STATUS, TONE_PILL } from "../plugins/status";
import { useDashboardSummary } from "./queries";

const VERIFIER_DOT = { healthy: "bg-emerald-400", degraded: "bg-amber-400", unavailable: "bg-rose-400" } as const;
const MAX_SHOWN = 5;

export function IntegrationsStrip() {
  // The same catalog and the same status vocabulary as the Plugins screen, so
  // the two views cannot disagree about an integration's state.
  const { data, isPending, isError, error } = usePluginCatalog();
  // Verifier health comes from the (unscoped) summary, shared with the sidebar.
  const { data: summary } = useDashboardSummary({});

  if (isPending) {
    return <div aria-label="Loading integrations" className="h-24 rounded-btn bg-app-surface border border-app-border animate-pulse" />;
  }

  if (isError) {
    return (
      <div role="alert" className="rounded-btn bg-app-surface border border-app-border px-4 py-3 shadow-card">
        <p className="text-sm font-medium text-fg">Couldn&apos;t load integration health</p>
        <p className="text-xs text-fg-muted mt-1">
          {error instanceof Error ? error.message : "The integrations endpoint is unavailable."}
        </p>
      </div>
    );
  }

  const installed = installedOf(data.plugins).map((p) => ({ p, status: INSTALLATION_STATUS[p.installation!.status] }));
  const needAttention = installed.filter((x) => x.status.tone !== "ok").length;
  // Problems first: the strip is for noticing what needs attention.
  const shown = [...installed].sort((a, b) => a.status.rank - b.status.rank).slice(0, MAX_SHOWN);

  return (
    <div className="bg-app-surface border border-app-border rounded-btn px-4 py-3 space-y-3 shadow-card">
      <div className="flex items-center justify-between text-xs">
        <div className="flex items-center gap-2">
          <span className="font-medium text-fg">Integration health</span>
          <span className="text-fg-muted" aria-hidden>•</span>
          <span className="text-fg-secondary">
            {installed.length} connected •{" "}
            {needAttention === 0 ? "no action required" : `${needAttention} ${needAttention === 1 ? "needs" : "need"} attention`}
          </span>
          {summary && (
            <>
              <span className="hidden lg:inline text-fg-faint" aria-hidden>|</span>
              <span className="hidden lg:inline-flex items-center gap-1.5 text-fg-muted">
                <span className={`h-1.5 w-1.5 rounded-full ${VERIFIER_DOT[summary.verifier]}`} aria-hidden />
                <span>Verification {summary.verifier}</span>
              </span>
            </>
          )}
        </div>
        <Link href="/plugins" className="text-xs font-medium text-ok-strong hover:underline flex items-center gap-1">
          <span>View all integrations</span>
          <span aria-hidden>→</span>
        </Link>
      </div>

      {installed.length === 0 ? (
        <p className="text-xs text-fg-muted">
          No integrations are installed yet. Install Kubernetes and Prometheus to start collecting evidence.
        </p>
      ) : (
        <div className="hidden xl:grid grid-cols-5 gap-2">
          {shown.map(({ p, status }) => (
            <Link
              key={p.id}
              href={`/plugins/${encodeURIComponent(p.id)}`}
              title={p.installation!.status_reason}
              className="bg-app-subcard border border-app-border rounded-btn p-2.5 flex items-center justify-between gap-2 hover:border-app-border-strong transition-colors"
            >
              <div className="min-w-0">
                <div className="text-xs font-medium text-fg truncate">{p.name}</div>
                <div className="text-[11px] text-fg-muted truncate">{p.capabilities.map((c) => CAPABILITY_LABELS[c]).join(", ")}</div>
              </div>
              <span className={`px-2 py-0.5 rounded border text-[10px] font-medium shrink-0 ${TONE_PILL[status.tone]}`}>
                {status.label}
              </span>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}