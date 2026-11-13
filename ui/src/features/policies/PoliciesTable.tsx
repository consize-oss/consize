"use client";

import { Box, BrainCircuit, Database, GitPullRequest, ShieldCheck, Zap } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { POLICY_PAGE_SIZE, usePolicyInventory } from "./policyInventory";
import { matchedPercent, policyAge, STATUS_LABELS } from "./policyInventoryModel";
import type { Policy } from "./queries";

const KIND: Record<Policy["kind"], { icon: LucideIcon; tile: string }> = {
  guardrail: { icon: ShieldCheck, tile: "border-ok-line bg-ok-bg text-ok-strong" },
  automation: { icon: Zap, tile: "border-info-line bg-info-bg text-info-fg" },
  database: { icon: Database, tile: "border-warn-line bg-warn-bg text-warn-fg" },
  gitops: { icon: GitPullRequest, tile: "border-info-line bg-info-bg text-info-fg" },
  storage: { icon: Box, tile: "border-violet-500/25 bg-violet-500/10 text-violet-500" },
  ai: { icon: BrainCircuit, tile: "border-purple-500/25 bg-purple-500/10 text-purple-500" },
};

// Colour groups the action path by how a change reaches production.
const PATH_TEXT: Record<Policy["action_path"]["method"], string> = {
  gitops_pr: "text-info-fg",
  terraform_pr: "text-info-fg",
  direct_apply: "text-ok-strong",
  gateway_route: "text-ok-strong",
  scheduled: "text-warn-fg",
};

const STATUS_PILL: Record<Policy["status"], string> = {
  active: "border-ok-line bg-ok-bg text-ok-strong",
  draft: "border-info-line bg-info-bg text-info-fg",
  disabled: "border-app-border bg-app-surface-alt text-fg-secondary",
};

const HEADINGS = ["Policy", "Scope", "Safety rules", "Action path", "Verification", "Coverage", "Status", "Modified"];

export function PoliciesTable({ resultsId }: { resultsId: string }) {
  const { items, asOf, isLoading, isError, isRetrying, retry, hasFilters } = usePolicyInventory();

  if (isLoading) {
    return (
      <div role="status" aria-label="Loading policies" className="space-y-3 p-4">
        <span className="sr-only">Loading policies...</span>
        {Array.from({ length: POLICY_PAGE_SIZE }, (_, i) => (
          <div key={i} aria-hidden="true" className="h-14 animate-pulse rounded bg-app-surface-alt motion-reduce:animate-none" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div role="alert" className="p-6">
        <p className="text-sm font-medium text-fg">Couldn&apos;t load policies</p>
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
        <p className="text-sm font-medium text-fg">{hasFilters ? "No policies match these filters" : "No policies defined yet"}</p>
        <p className="mt-1 text-xs text-fg-muted">
          {hasFilters ? "Try different filters." : "Until a policy exists, no change can be evaluated against one."}
        </p>
      </div>
    );
  }

  return (
    <div
      id={resultsId}
      role="region"
      aria-label="Policies table"
      tabIndex={0}
      className="overflow-x-auto focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent-500"
    >
      <table className="w-full min-w-[1080px] table-fixed border-collapse text-left">
        <caption className="sr-only">
          Policy inventory: scope, safety rules, action path, verification, coverage, status, and last modification.
        </caption>
        <colgroup>
          <col className="w-[21%]" />
          <col className="w-[13%]" />
          <col className="w-[14%]" />
          <col className="w-[12%]" />
          <col className="w-[13%]" />
          <col className="w-[12%]" />
          <col className="w-[7%]" />
          <col className="w-[8%]" />
        </colgroup>
        <thead className="border-b border-app-border bg-app-bg/60 text-[10px] uppercase tracking-wide text-fg-muted">
          <tr className="h-10">
            {HEADINGS.map((h) => (
              <th key={h} scope="col" className="px-3 font-semibold">{h}</th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-app-border text-[11px] leading-4">
          {items.map((policy) => (
            <PolicyRow key={policy.id} policy={policy} now={asOf} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Pair({ summary, detail, strong }: { summary: string; detail?: string; strong?: boolean }) {
  return (
    <>
      <span className={"block break-words " + (strong ? "font-semibold text-fg" : "text-fg")}>{summary}</span>
      {detail && <span className="mt-0.5 block break-words text-[10px] text-fg-muted">{detail}</span>}
    </>
  );
}

function PolicyRow({ policy, now }: { policy: Policy; now: number }) {
  const kind = KIND[policy.kind];
  const Icon = kind.icon;
  const percent = matchedPercent(policy.coverage.matched_ratio);
  // A draft isn't enforcing, so its coverage is a preview, not a fact.
  const coverageDetail = policy.status === "active" ? `${percent}% matched` : `${percent}% would match`;

  return (
    <tr className="align-middle transition-colors hover:bg-app-surface-alt/50">
      <th scope="row" className="px-3 py-3.5 text-left font-normal">
        <div className="flex items-center gap-2.5">
          <span aria-hidden="true" className={"flex h-7 w-7 shrink-0 items-center justify-center rounded border " + kind.tile}>
            <Icon className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <p className="break-words font-semibold text-fg">{policy.name}</p>
            <p className="mt-0.5 break-words text-[10px] text-fg-muted">
              {policy.version} · {policy.owner_team}
            </p>
          </div>
        </div>
      </th>
      <td className="px-3 py-3.5"><Pair summary={policy.scope.summary} detail={policy.scope.detail} /></td>
      <td className="px-3 py-3.5"><Pair summary={policy.safety.summary} detail={policy.safety.detail} /></td>
      <td className="px-3 py-3.5">
        <span className={"block break-words font-medium " + PATH_TEXT[policy.action_path.method]}>{policy.action_path.label}</span>
        <span className="mt-0.5 block break-words text-[10px] text-fg-muted">{policy.action_path.approval}</span>
      </td>
      <td className="px-3 py-3.5"><Pair summary={policy.verification.summary} detail={policy.verification.detail} /></td>
      <td className="px-3 py-3.5">
        <span className="block font-semibold text-fg font-num">{policy.coverage.matched.toLocaleString("en-US")} resources</span>
        <span
          className={
            "mt-0.5 block text-[10px] font-num " +
            (policy.coverage.needs_attention ? "font-medium text-warn-fg" : "text-fg-muted")
          }
        >
          {coverageDetail}
        </span>
      </td>
      <td className="px-3 py-3.5">
        <span className={"inline-flex rounded border px-2 py-0.5 text-[10px] font-medium " + STATUS_PILL[policy.status]}>
          {STATUS_LABELS[policy.status]}
        </span>
      </td>
      <td className="px-3 py-3.5">
        <time dateTime={policy.modified_at} title={policy.modified_at} className="block text-fg-secondary">
          {policyAge(policy.modified_at, now)}
        </time>
        <span className="mt-0.5 block break-words text-[10px] text-fg-muted">{policy.modified_by}</span>
      </td>
    </tr>
  );
}