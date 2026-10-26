"use client";

import Link from "next/link";
import { Box, BrainCircuit, ChevronRight, Cpu, Database, Hexagon } from "lucide-react";
import { formatMoney, titleCase } from "@/lib/format";
import type { RecommendationListItem } from "@/schemas";
import { RecommendationAction } from "./RecommendationAction";



type RecommendationsTableProps = {
  items: RecommendationListItem[];
  isLoading: boolean;
  isError: boolean;
  isRetrying: boolean;
  hasFilters: boolean;
  onRetry: () => void;
};

const RISK_STYLES: Record<RecommendationListItem["risk"], string> = {
  low: "border-badge-low/25 bg-badge-low/10 text-badge-low-text",
  medium: "border-badge-medium/25 bg-badge-medium/10 text-badge-medium-text",
  high: "border-badge-high/25 bg-badge-high/10 text-badge-high-text",
};

const RESOURCE_STYLES = {
  compute: { icon: Hexagon, color: "border-status-info/25 bg-status-info/10 text-status-info" },
  database: { icon: Database, color: "border-badge-medium/25 bg-badge-medium/10 text-badge-medium-text" },
  storage: { icon: Box, color: "border-violet-500/25 bg-violet-500/10 text-violet-500" },
  ai: { icon: BrainCircuit, color: "border-purple-500/25 bg-purple-500/10 text-purple-500" },
  gpu: { icon: Cpu, color: "border-badge-high/25 bg-badge-high/10 text-badge-high-text" },
};

export function RecommendationsTable({ items, isLoading, isError, isRetrying, hasFilters, onRetry }: RecommendationsTableProps) {
  // The Screen supplies this page's rows and loading state from one shared result.
  if (isLoading) {
    return (
      <div role="status" aria-label="Loading recommendations" className="space-y-3 p-4">
        <span className="sr-only">Loading recommendations…</span>
        {Array.from({ length: 5 }, (_, index) => (
          <div key={index} aria-hidden="true" className="h-14 animate-pulse rounded bg-app-surface-alt motion-reduce:animate-none" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div role="alert" className="p-6">
        <p className="text-sm font-medium text-fg">Couldn&apos;t load recommendations</p>
        <p className="mt-1 text-xs text-fg-muted">Please try again.</p>
        <button
          type="button"
          onClick={onRetry}
          disabled={isRetrying}
          aria-busy={isRetrying}
          className="mt-3 h-8 cursor-pointer rounded-md border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-wait disabled:opacity-60"
        >
          {isRetrying ? "Retrying…" : "Retry"}
        </button>
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div role="status" className="px-4 py-12 text-center">
        <p className="text-sm font-medium text-fg">
          {hasFilters ? "No recommendations match these filters" : "No recommendations yet"}
        </p>
        <p className="mt-1 text-xs text-fg-muted">
          {hasFilters ? "Reset the filters to see everything." : "Recommendations will appear here when available."}
        </p>
      </div>
    );
  }

  return (
    // Keep columns readable on smaller screens; keyboard users can also scroll this region.
    <div role="region" aria-label="Recommendations table" tabIndex={0} className="overflow-x-auto focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent-500">
      <table className="w-full min-w-[1040px] table-fixed border-collapse text-left">
        <caption className="sr-only">Optimization recommendations, proposed changes, savings, risk, confidence, and available actions.</caption>
        <colgroup>
          <col className="w-[26%]" />
          <col className="w-[11%]" />
          <col className="w-[10%]" />
          <col className="w-[14%]" />
          <col className="w-[10%]" />
          <col className="w-[6%]" />
          <col className="w-[9%]" />
          <col className="w-[14%]" />
        </colgroup>
        <thead className="border-b border-app-border bg-app-bg/60 text-[10px] tracking-wide text-fg-muted uppercase">
          <tr className="h-10">
            {["Resource", "Environment", "Owner", "Recommendation", "Est. savings", "Risk", "Confidence", "Action"].map((heading) => (
              <th key={heading} scope="col" className={`px-3 font-semibold ${heading === "Action" ? "text-center" : ""}`}>{heading}</th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-app-border text-[11px] leading-4">
          {items.map((recommendation) => <RecommendationRow key={recommendation.id} recommendation={recommendation} />)}
        </tbody>
      </table>
    </div>
  );
}

function RecommendationRow({ recommendation: rec }: { recommendation: RecommendationListItem }) {
  const resourceStyle = RESOURCE_STYLES[rec.resource.kind ?? "compute"];
  const ResourceIcon = resourceStyle.icon;
  const savings = formatMoney(rec.estimated_savings);
  const resourceDescription = rec.resource.description
    ?? (rec.resource.namespace ? `ns: ${rec.resource.namespace}` : rec.resource.provider);
  const environment = rec.resource.environment === "datalake" ? "DataLake" : titleCase(rec.resource.environment);

  return (
    <tr className="align-middle transition-colors hover:bg-app-surface-alt/50">
      <th scope="row" className="px-3 py-3.5 text-left font-normal">
        <div className="flex items-center gap-2.5">
          <span aria-hidden="true" className={`flex h-7 w-7 shrink-0 items-center justify-center rounded border ${resourceStyle.color}`}>
            <ResourceIcon className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
              <Link href={`/recommendations/${encodeURIComponent(rec.id)}`} className="break-words rounded font-[650] text-fg hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500">
                {rec.resource.name}
              </Link>
              {rec.resource.inspect && <span className="rounded border border-badge-low/25 bg-badge-low/10 px-1 text-[8px] leading-3 text-badge-low-text uppercase">Inspect</span>}
            </div>
            <p className="mt-0.5 text-[10px] text-fg-muted">{resourceDescription}</p>
          </div>
        </div>
      </th>
      <td className="px-3 py-3.5">
        <span className="flex items-center gap-1 font-[650] text-fg">
          <span aria-hidden="true" className={`h-1.5 w-1.5 shrink-0 rounded-full ${rec.resource.environment === "production" ? "bg-status-high" : "bg-status-info"}`} />
          {environment}
        </span>
        <span className="mt-0.5 block text-[10px] text-fg-muted">{rec.resource.scope ?? rec.resource.region ?? "—"}</span>
      </td>
      <td className="break-words px-3 py-3.5 text-fg-secondary">{rec.owner}</td>
      <td className="px-3 py-3.5">
        <span className="block text-[10px] font-[650] text-fg">{rec.change_summary?.current ?? "—"}</span>
        {rec.change_summary?.proposed && (
          <span className="mt-0.5 flex items-start gap-0.5 text-[10px] text-ok-fg">
            <ChevronRight aria-hidden="true" className="mt-0.5 h-3 w-3 shrink-0" />
            <span><span className="sr-only">Suggested: </span>{rec.change_summary.proposed}</span>
          </span>
        )}
      </td>
      <td className="px-3 py-3.5">
        <span className="whitespace-nowrap text-[13px] font-[650] text-ok-strong">
          {rec.estimated_savings.amount_minor > 0 ? "+" : ""}{savings.display}
          <span className="text-[10px] font-normal text-fg-muted">/mo</span>
        </span>
        <span className="mt-0.5 block text-[10px] text-fg-muted">{rec.savings_note ?? "—"}</span>
      </td>
      <td className="px-3 py-3.5">
        <span className={`inline-flex rounded border px-1.5 py-0.5 text-[9px] leading-3 font-[650] ${RISK_STYLES[rec.risk]}`}>{titleCase(rec.risk)}</span>
      </td>
      <td className="px-3 py-3.5">
        {/* A percentage is a separate value, never a conversion from the legacy high/medium/low label. */}
        <span className="block font-[650] text-fg">{rec.confidence_percentage == null ? "—" : `${rec.confidence_percentage}%`}</span>
        <span className="mt-0.5 block text-[10px] text-fg-muted">{rec.confidence_note ?? "—"}</span>
      </td>
      <td className="px-3 py-3.5 text-center"><RecommendationAction recommendation={rec} /></td>
    </tr>
  );
}
