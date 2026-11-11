import { getClientPage } from "@/lib/pagination";
import { toCsv } from "@/lib/csv";
import type { Policy } from "./queries";

export const POLICY_PAGE_SIZE = 7;

export type PolicyFilters = {
  q: string;
  status: string;
  environment: string;
  path: string;
  owner: string;
  coverage: string;
};
export const FILTER_KEYS = ["q", "status", "environment", "path", "owner", "coverage"] as const;

export const STATUS_LABELS: Record<Policy["status"], string> = {
  active: "Active",
  draft: "Draft",
  disabled: "Disabled",
};
export const COVERAGE_LABELS: Record<string, string> = {
  full: "Fully matched",
  partial: "Partially matched",
};

export function coverageState(policy: Policy): "full" | "partial" {
  return policy.coverage.matched_ratio >= 1 ? "full" : "partial";
}

// 0.996 must not read as "100%": floor, except a true 1.
export function matchedPercent(ratio: number) {
  return ratio >= 1 ? 100 : Math.floor(ratio * 100);
}

export function policyAge(iso: string, now: number) {
  const minutes = Math.max(0, Math.floor((now - Date.parse(iso)) / 60_000));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d ago`;
  if (days < 365) return `${Math.floor(days / 7)}w ago`;
  return `${Math.floor(days / 365)}y ago`;
}

const unique = (values: string[]) => [...new Set(values)].sort();

function searchText(p: Policy) {
  return [
    p.id, p.name, p.version, p.owner_team, p.modified_by,
    p.scope.summary, p.scope.detail, p.safety.summary, p.safety.detail,
    p.action_path.label, p.action_path.approval, p.verification.summary, p.verification.detail,
  ].join(" ").toLowerCase();
}

export function buildPolicyInventory(
  policies: readonly Policy[],
  filters: PolicyFilters,
  requestedPage: number,
) {
  const terms = filters.q.trim().toLowerCase().split(/\s+/).filter(Boolean);
  // Everything except status: the All / Active / Draft counts must not
  // collapse to zero for the tabs you're not on.
  const matching = policies.filter((p) => {
    if (filters.environment && !p.scope.environments.includes(filters.environment)) return false;
    if (filters.path && p.action_path.method !== filters.path) return false;
    if (filters.owner && p.owner_team !== filters.owner) return false;
    if (filters.coverage && coverageState(p) !== filters.coverage) return false;
    if (terms.length === 0) return true;
    const text = searchText(p);
    return terms.every((term) => text.includes(term));
  });
  const counts = {
    all: matching.length,
    active: matching.filter((p) => p.status === "active").length,
    draft: matching.filter((p) => p.status === "draft").length,
  };
  const filteredItems = filters.status ? matching.filter((p) => p.status === filters.status) : matching;

  const pathLabels: Record<string, string> = {};
  for (const p of policies) pathLabels[p.action_path.method] = p.action_path.label;

  return {
    filteredItems,
    counts,
    options: {
      statuses: unique(policies.map((p) => p.status)),
      environments: unique(policies.flatMap((p) => p.scope.environments)),
      paths: unique(policies.map((p) => p.action_path.method)),
      pathLabels,
      owners: unique(policies.map((p) => p.owner_team)),
      coverage: ["full", "partial"],
    },
    pagination: getClientPage(filteredItems, requestedPage, POLICY_PAGE_SIZE),
  };
}

export function policyMatrixCSV(policies: readonly Policy[]) {
  return toCsv([
    ["Policy ID", "Policy", "Version", "Owner team", "Status", "Scope", "Scope details",
      "Safety rules", "Safety details", "Action path", "Approval", "Verification",
      "Verification details", "Matched resources", "Matched", "Modified at", "Modified by"],
    ...policies.map((p) => [
      p.id, p.name, p.version, p.owner_team, STATUS_LABELS[p.status], p.scope.summary, p.scope.detail ?? "",
      p.safety.summary, p.safety.detail ?? "", p.action_path.label, p.action_path.approval,
      p.verification.summary, p.verification.detail ?? "", String(p.coverage.matched),
      matchedPercent(p.coverage.matched_ratio) + "%", p.modified_at, p.modified_by,
    ]),
  ]);
}