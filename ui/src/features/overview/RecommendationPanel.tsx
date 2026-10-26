"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Box, Cpu, Database, Folder, Server } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { formatMoney } from "@/lib/format";
import type { RecommendationListItem } from "@/schemas";
import { summaryParamsOf, useScope } from "../scope/useScope";
import { useDashboardSummary, useRecommendationList } from "./queries";
import { RecommendationAction } from "../recommendations/RecommendationAction";

type Attention = "all" | "approvals" | "high-risk" | "blocked";
const ATTENTION: { key: Attention; label: string; dot?: string; empty: string }[] = [
  { key: "all", label: "All", empty: "No recommendations yet." },
  { key: "approvals", label: "Approvals", dot: "bg-amber-400", empty: "Nothing is waiting for approval." },
  { key: "high-risk", label: "High risk", dot: "bg-rose-400", empty: "No high-risk recommendations." },
  { key: "blocked", label: "Blocked", dot: "bg-rose-400", empty: "No blocked recommendations require attention." },
];

// The segmented control maps onto API filters; the server does the filtering.
function filtersFor(a: Attention): Record<string, string> {
  if (a === "approvals") return { status: "pending_approval" };
  if (a === "high-risk") return { risk: "high" };
  if (a === "blocked") return { status: "blocked" };
  return {};
}

const RISK: Record<RecommendationListItem["risk"], { label: string; pill: string }> = {
  low: { label: "Low", pill: "bg-ok-bg text-ok-fg border-ok-line" },
  medium: { label: "Medium", pill: "bg-warn-bg text-warn-fg border-warn-line" },
  high: { label: "High", pill: "bg-bad-bg text-bad-fg border-bad-line" },
};

const VISUAL: Record<string, { icon: LucideIcon; tile: string; glyph: string }> = {
  Deployment: { icon: Box, tile: "bg-info-bg border-info-line", glyph: "text-info-fg" },
  "Aurora cluster": { icon: Database, tile: "bg-warn-bg border-warn-line", glyph: "text-warn-fg" },
  "S3 bucket": { icon: Folder, tile: "bg-violet-500/10 border-violet-500/25", glyph: "text-violet-500" },
  "GPU node group": { icon: Cpu, tile: "bg-bad-bg border-bad-line", glyph: "text-bad-fg" },
  "Worker pool": { icon: Server, tile: "bg-ok-bg border-ok-line", glyph: "text-ok-strong" },
};
const FALLBACK_VISUAL = { icon: Box, tile: "bg-app-surface-alt border-app-border", glyph: "text-fg-muted" };

const PROVIDER_SHORT: Record<string, string> = { kubernetes: "k8s", aws: "AWS" };
export function RecommendationPanel() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { scope, range, update, carry } = useScope();

  const raw = searchParams.get("attention");
  const attention: Attention = ATTENTION.some((a) => a.key === raw) ? (raw as Attention) : "all";
  const active = ATTENTION.find((a) => a.key === attention)!;

  // Scope + attention are server-side filters; sort=priority + limit asks for
  // the backend's ranking, not a client-side sort (ticket: "backend ranking").
  const { data, isPending, isError, error, isRefetching } = useRecommendationList({
    ...scope,
    ...filtersFor(attention),
    sort: "priority",
    limit: "5",
  });
  const { data: summary } = useDashboardSummary(summaryParamsOf(scope, range));

  const items = data?.items ?? [];

  return (
    <div aria-busy={isRefetching} className="bg-app-surface border border-app-border rounded-btn overflow-hidden shadow-card">
      <div className="p-4 border-b border-app-border flex flex-wrap items-center justify-between gap-3 bg-app-toolbar">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold text-fg">Prioritized recommendations</h2>
            {data && (
              <span className="px-2 py-0.5 rounded text-xs font-medium bg-app-surface-alt text-fg-secondary border border-app-border">
                {items.length} shown
              </span>
            )}
          </div>
          <p className="text-xs text-fg-muted mt-0.5">
            Prioritized by projected savings, risk tier, and policy eligibility.
          </p>
        </div>

        <div
          role="group"
          aria-label="Recommendation attention categories"
          className="flex items-center gap-1 rounded-btn border border-app-border bg-app-inset p-1"
        >
          {ATTENTION.map((a) => {
            const on = a.key === attention;
            return (
              <button
                key={a.key}
                type="button"
                aria-pressed={on}
                onClick={() => update({ attention: a.key === "all" ? undefined : a.key })}
                className={`flex h-7 items-center gap-1.5 rounded-md px-3 text-xs transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-500 ${
                  on
                    ? "bg-ok-bg-strong text-ok-fg font-semibold"
                    : "text-fg-secondary font-medium hover:bg-app-surface-alt hover:text-fg"
                }`}
              >
                {a.dot && <span className={`h-1.5 w-1.5 rounded-full ${a.dot}`} aria-hidden />}
                {a.label}
              </button>
            );
          })}
        </div>
      </div>

      {isPending ? (
        <div aria-label="Loading recommendations" className="p-4 space-y-3 animate-pulse">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="h-12 bg-app-surface-alt rounded" />
          ))}
        </div>
      ) : isError ? (
        <div role="alert" className="p-6">
          <p className="text-sm font-medium text-fg">Couldn&apos;t load recommendations</p>
          <p className="text-xs text-fg-muted mt-1">
            {error instanceof Error ? error.message : "The recommendation endpoint is unavailable."}
          </p>
        </div>
      ) : items.length === 0 ? (
        <div className="px-4 py-10 text-center">
          <p className="text-sm font-medium text-fg">{active.empty}</p>
          {attention === "all" && (
            <p className="text-xs text-fg-muted mt-1">
              Evidence is being collected. Recommendations appear once enough history is available.
            </p>
          )}
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse text-xs">
            <thead>
              <tr className="h-9 bg-app-thead border-b border-app-border text-fg-muted uppercase tracking-wider text-[11px] font-medium">
                <th className="px-4 font-medium">Resource</th>
                <th className="px-3 font-medium">Owner / policy</th>
                <th className="px-3 font-medium">Change risk</th>
                <th className="px-3 font-medium text-right">Est. savings</th>
                <th className="px-4 font-medium text-right">Action</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-app-border">
              {items.map((rec) => {
                const visual = VISUAL[rec.resource.type] ?? FALLBACK_VISUAL;
                const Icon = visual.icon;
                const risk = RISK[rec.risk];
                const money = formatMoney(rec.estimated_savings);
                const href = `/recommendations/${rec.id}`;
                const meta = [PROVIDER_SHORT[rec.resource.provider] ?? rec.resource.provider, rec.resource.source, rec.target_summary].join(" • ");
                return (
                  <tr
                    key={rec.id}
                    // Mouse convenience: the real keyboard path is the link
                    // on the resource name and the action button below.
                    onClick={() => router.push(href)}
                    className="cursor-pointer hover:bg-app-hover transition-colors"
                  >
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5">
                        <div className={`w-7 h-7 rounded border flex items-center justify-center shrink-0 ${visual.tile}`}>
                          <Icon className={`w-3.5 h-3.5 ${visual.glyph}`} aria-hidden />
                        </div>
                        <div className="min-w-0">
                          <Link
                            href={href}
                            onClick={(e) => e.stopPropagation()}
                            className="block font-mono text-xs font-medium text-fg truncate hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-500 rounded"
                          >
                            {rec.resource.name}
                          </Link>
                          <div className="font-mono text-[10px] text-fg-muted truncate mt-0.5">{meta}</div>
                        </div>
                      </div>
                    </td>
                    <td className="px-3 py-3">
                      <div className="text-fg font-medium">{rec.owner}</div>
                      <div className="font-mono text-[10px] text-fg-muted mt-0.5">{rec.policy_id}</div>
                    </td>
                    <td className="px-3 py-3 whitespace-nowrap">
                      <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium border ${risk.pill}`}>
                        {risk.label}
                      </span>
                    </td>
                    <td className="px-3 py-3 text-right whitespace-nowrap">
                      <span title={`Estimate basis: ${money.basisLabel}`} className="text-ok-strong font-semibold font-num text-xs">
                        {money.display}
                      </span>
                      <span className="text-fg-muted text-[10px]">/mo</span>
                    </td>
                    <td className="px-4 py-3 text-right whitespace-nowrap">
                      <RecommendationAction recommendation={rec} variant="overview" />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className="px-4 py-3 bg-app-toolbar border-t border-app-border flex items-center justify-between text-xs text-fg-muted">
        <span>
          {attention === "all" && summary
            ? `Showing ${items.length} of ${summary.open_recommendations} recommendations`
            : `Showing ${items.length} ${active.label.toLowerCase()} recommendation${items.length === 1 ? "" : "s"}`}
        </span>
        <Link href={carry("/recommendations")} className="font-medium text-ok-strong hover:underline flex items-center gap-1">
          <span>View all recommendations</span>
          <span aria-hidden>→</span>
        </Link>
      </div>
    </div>
  );
}
//