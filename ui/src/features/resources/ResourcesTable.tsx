"use client";

import Link from "next/link";
import { Box, BrainCircuit, Cpu, Database, Hexagon } from "lucide-react";
import { relativeTime, titleCase } from "@/lib/format";
import { RESOURCE_PAGE_SIZE, useResourceInventory, type ResourceInventoryItem } from "./resourceInventory";

const RESOURCE_STYLES = {
  compute: { icon: Hexagon, color: "border-status-info/25 bg-status-info/10 text-status-info" },
  database: { icon: Database, color: "border-badge-medium/25 bg-badge-medium/10 text-badge-medium-text" },
  storage: { icon: Box, color: "border-violet-500/25 bg-violet-500/10 text-violet-500" },
  ai: { icon: BrainCircuit, color: "border-purple-500/25 bg-purple-500/10 text-purple-500" },
  gpu: { icon: Cpu, color: "border-badge-high/25 bg-badge-high/10 text-badge-high-text" },
};

const TELEMETRY = {
  healthy: { label: "Healthy", color: "text-ok-strong" },
  stale: { label: "Stale", color: "text-bad-fg" },
  unmonitored: { label: "Unmonitored", color: "text-warn-fg" },
  unknown: { label: "Unknown", color: "text-fg-muted" },
};

export function ResourcesTable() {
  const { items, isLoading, isError, isRetrying, retry, hasFilters, asOf } = useResourceInventory();

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading resources" className="space-y-3 p-4">
        <span className="sr-only">Loading resources...</span>
        {Array.from({ length: RESOURCE_PAGE_SIZE }, (_, index) => (
          <div key={index} aria-hidden="true" className="h-14 animate-pulse rounded bg-app-surface-alt motion-reduce:animate-none" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div role="alert" className="p-6">
        <p className="text-sm font-medium text-fg">Couldn&apos;t load resources</p>
        <p className="mt-1 text-xs text-fg-muted">Please try again.</p>
        <button
          type="button"
          onClick={() => void retry()}
          disabled={isRetrying}
          aria-busy={isRetrying}
          className="mt-3 h-8 cursor-pointer rounded-md border border-app-border px-3 text-xs text-fg hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-wait"
        >
          {isRetrying ? "Retrying..." : "Retry"}
        </button>
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div role="status" className="px-4 py-12 text-center">
        <p className="text-sm font-medium text-fg">{hasFilters ? "No resources match these filters" : "No resources discovered yet"}</p>
        <p className="mt-1 text-xs text-fg-muted">{hasFilters ? "Try different filters or change the infrastructure scope." : "Resources will appear here when inventory is available."}</p>
      </div>
    );
  }

  return (
    <div id="resource-inventory-results" role="region" aria-label="Resources table" tabIndex={0} className="overflow-x-auto focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent-500">
      <table className="w-full min-w-[1000px] table-fixed border-collapse text-left">
        <caption className="sr-only">Resource inventory, providers, environments, owners, telemetry, policy coverage, and optimization status.</caption>
        <colgroup>
          <col className="w-[29%]" />
          <col className="w-[11%]" />
          <col className="w-[11%]" />
          <col className="w-[11%]" />
          <col className="w-[16%]" />
          <col className="w-[9%]" />
          <col className="w-[13%]" />
        </colgroup>
        <thead className="border-b border-app-border bg-app-bg/60 text-[10px] uppercase tracking-wide text-fg-muted">
          <tr className="h-10">
            {["Resource", "Provider", "Environment", "Owner", "Telemetry", "Policy", "Optimization status"].map((heading) => (
              <th key={heading} scope="col" className="px-3 font-semibold">{heading}</th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-app-border text-[11px] leading-4">
          {items.map((resource) => <ResourceRow key={resource.id} resource={resource} now={asOf} />)}
        </tbody>
      </table>
    </div>
  );
}

function ResourceRow({ resource, now }: { resource: ResourceInventoryItem; now: number }) {
  const details = resource.inventory;
  const style = RESOURCE_STYLES[details.kind ?? "compute"];
  const ResourceIcon = style.icon;
  const telemetry = TELEMETRY[details.telemetry?.status ?? "unknown"];
  const coverage = details.policy_coverage ?? "unknown";
  const recommendationIds = details.recommendation_ids;
  const optimizationStatus = details.optimization_status ?? (recommendationIds?.length ? "recommendations" : "unknown");
  const checkedAt = details.telemetry?.checked_at;
  const secondsSinceCheck = checkedAt ? Math.max(0, Math.floor((now - new Date(checkedAt).getTime()) / 1000)) : null;
  const telemetryAge = checkedAt && secondsSinceCheck != null
    ? secondsSinceCheck < 60 ? (secondsSinceCheck === 0 ? "just now" : secondsSinceCheck + "s ago") : relativeTime(checkedAt, now)
    : null;
  const environment = resource.environment === "datalake" ? "DataLake" : titleCase(resource.environment);
  const provider = resource.provider === "aws" ? "AWS" : titleCase(resource.provider);

  return (
    <tr className="align-middle transition-colors hover:bg-app-surface-alt/50">
      <th scope="row" className="px-3 py-3.5 text-left font-normal">
        <div className="flex items-center gap-2.5">
          <span aria-hidden="true" className={"flex h-7 w-7 shrink-0 items-center justify-center rounded border " + style.color}>
            <ResourceIcon className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <p className="break-words font-semibold text-fg">{resource.name}</p>
            <p className="mt-0.5 break-words text-[10px] text-fg-muted">{details.description ?? resource.type}</p>
          </div>
        </div>
      </th>
      <td className="break-words px-3 py-3.5 font-medium text-fg">{provider}</td>
      <td className="px-3 py-3.5">
        <span className="flex items-center gap-1 text-fg-secondary">
          <span aria-hidden="true" className={"h-1.5 w-1.5 shrink-0 rounded-full " + (resource.environment === "production" ? "bg-rose-400" : "bg-sky-400")} />
          {environment}
        </span>
      </td>
      <td className="break-words px-3 py-3.5">
        {resource.owner.trim() ? <span className="block text-fg-secondary">{resource.owner}</span> : (
          <span className="text-fg-muted" title="No owner assigned">
            <span aria-hidden="true">{"\u2014"}</span>
            <span className="sr-only">No owner assigned</span>
          </span>
        )}
        {resource.owner.trim() && details.owner_note && details.owner_note !== resource.owner && (
          <span className="mt-0.5 block text-[10px] text-fg-muted">{details.owner_note}</span>
        )}
      </td>
      <td className="px-3 py-3.5">
        <span className={"block font-medium " + telemetry.color}>{telemetry.label}</span>
        {(details.telemetry?.checked_at || details.telemetry?.source) && (
          <p className="mt-0.5 break-words text-[10px] text-fg-muted">
            {details.telemetry.checked_at && (
              <time dateTime={details.telemetry.checked_at} title={details.telemetry.checked_at}>
                {telemetryAge}
              </time>
            )}
            {details.telemetry.checked_at && details.telemetry.source && " · "}
            {details.telemetry.source}
          </p>
        )}
      </td>
      <td className="px-3 py-3.5">
        {coverage === "covered" ? (
          <span className="inline-flex rounded border border-ok-line bg-ok-bg px-1.5 py-0.5 text-[10px] font-medium text-ok-strong">Covered</span>
        ) : (
          <span className="text-fg-muted" title={coverage === "uncovered" ? "No policy coverage" : "Policy coverage is unknown"}>
            <span aria-hidden="true">{"\u2014"}</span>
            <span className="sr-only">{coverage === "uncovered" ? "No policy coverage" : "Policy coverage unknown"}</span>
          </span>
        )}
      </td>
      <td className="px-3 py-3.5">
        {optimizationStatus === "awaiting_evidence" ? (
          <span className="text-fg-secondary">Awaiting evidence</span>
        ) : optimizationStatus === "no_action_needed" ? (
          <span className="text-fg-secondary">No action needed</span>
        ) : optimizationStatus === "action_unsupported" ? (
          <span className="font-medium text-info-fg" title="An execution action is not supported for this resource">Action unsupported</span>
        ) : optimizationStatus === "recommendations" && recommendationIds != null ? (
          recommendationIds.length === 0 ? <span className="text-fg-secondary">No recommendations</span> : (
            <Link
              href={recommendationIds.length === 1 ? `/recommendations/${encodeURIComponent(recommendationIds[0])}` : "/recommendations"}
              className="rounded font-medium text-info-fg no-underline hover:no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
            >
              {recommendationIds.length} {recommendationIds.length === 1 ? "recommendation" : "recommendations"}
            </Link>
          )
        ) : <span className="text-fg-muted">Unknown</span>}
      </td>
    </tr>
  );
}
