"use client";

import Link from "next/link";
import { Check, Clock, GitPullRequest, Lock, RotateCcw, Zap } from "lucide-react";
import type { RecommendationListItem } from "@/schemas";

type Remediation = RecommendationListItem["available_remediations"][number];
type ActionDescription = { label: string; method: Remediation["method"]; requiresApproval: boolean };
type RecommendationActionProps = {
  recommendation: RecommendationListItem;
  variant?: "recommendations" | "overview";
};

const METHOD_ICONS = {
  review: Check,
  github_pull_request: GitPullRequest,
  schedule: Clock,
  direct_apply: Zap,
  recover: RotateCcw,
};

export function getRecommendationAction(rec: RecommendationListItem): ActionDescription {
  const enabled = rec.available_remediations.find((action) => action.enabled && action.method !== "review");
  if (enabled) return { label: enabled.label, method: enabled.method, requiresApproval: false };

  const review = rec.available_remediations.find((action) => action.method === "review");
  const requiresApproval = rec.available_remediations.some(
    (action) => !action.enabled && action.disabled_reason === "approval_required",
  );
  if (requiresApproval) {
    return { label: "Approval required", method: "review", requiresApproval: true };
  }
  return { label: review?.label ?? "Review", method: "review", requiresApproval: false };
}

export function RecommendationAction({ recommendation, variant = "recommendations" }: RecommendationActionProps) {
  const action = getRecommendationAction(recommendation);
  const ActionIcon = action.requiresApproval ? Lock : METHOD_ICONS[action.method];

  const overviewStyle = action.requiresApproval
    ? "bg-warn-bg hover:bg-warn-bg text-warn-fg border-warn-line"
    : action.method === "github_pull_request"
      ? "bg-ok-bg-strong hover:bg-ok-bg text-ok-fg border-ok-line"
      : "bg-app-subcard hover:bg-app-surface-alt text-fg-secondary hover:text-fg border-app-border";
  const labelColor = action.requiresApproval ? "text-badge-medium-text" : action.method === "schedule" ? "text-fg-secondary hover:text-fg" : "text-fg-secondary";
  const iconColor = action.requiresApproval
    ? "text-badge-medium-text"
    : action.method === "github_pull_request" ? "text-status-info"
      : action.method === "direct_apply" ? "text-accent-400" : "text-fg-secondary";
  const className = variant === "overview"
    ? `h-7 px-3 border rounded-btn text-xs font-medium inline-flex items-center gap-1.5 transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-500 ${overviewStyle}`
    : `inline-flex h-8 w-full max-w-36 items-center justify-center gap-1.5 whitespace-nowrap rounded-md border px-2 text-[10px] font-medium transition-colors hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 ${labelColor} ${action.requiresApproval ? "border-badge-medium/40 bg-badge-medium/10" : "border-app-border bg-app-surface"}`;

  return (
    <Link
      href={`/recommendations/${encodeURIComponent(recommendation.id)}`}
      onClick={(event) => event.stopPropagation()}
      title={`${action.label} — open recommendation details`}
      className={className}
    >
      <ActionIcon aria-hidden="true" className={`h-3 w-3 shrink-0 ${variant === "overview" || action.method === "schedule" ? "" : iconColor}`} />
      <span className={variant === "recommendations" ? "min-w-0 whitespace-normal leading-3" : undefined}>{action.label}</span>
    </Link>
  );
}
