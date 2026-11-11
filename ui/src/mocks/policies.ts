import { http, HttpResponse } from "msw";
import type { Policy, PolicySummary } from "@/features/policies/queries";
import type { Scenario } from "./handlers";

const daysAgo = (d: number) => new Date(Date.now() - d * 86_400_000).toISOString();

export function policyFixtures(): Policy[] {
  return [
    {
      id: "production-conservative", name: "production-conservative", version: "v2.4", owner_team: "Platform Engineering",
      kind: "guardrail", status: "active",
      scope: { summary: "Production", detail: "Kubernetes · Tier 1", environments: ["production"] },
      safety: { summary: "30% headroom", detail: "Max 25% per cycle" },
      action_path: { method: "gitops_pr", label: "GitOps PR", approval: "Approval required" },
      verification: { summary: "Datadog · 30m", detail: "Rollback enabled" },
      coverage: { matched: 614, matched_ratio: 1, needs_attention: false },
      modified_at: daysAgo(3), modified_by: "lead-infra-sre",
    },
    {
      id: "non-production-auto", name: "non-production-auto", version: "v1.8", owner_team: "Infrastructure",
      kind: "automation", status: "active",
      scope: { summary: "Staging and development", detail: "Kubernetes", environments: ["staging", "development"] },
      safety: { summary: "15% headroom", detail: "Max 40% per cycle" },
      action_path: { method: "direct_apply", label: "Direct apply", approval: "Pre-authorized" },
      verification: { summary: "Prometheus · 15m", detail: "Rollback enabled" },
      coverage: { matched: 382, matched_ratio: 0.96, needs_attention: false },
      modified_at: daysAgo(5), modified_by: "platform-admin",
    },
    {
      id: "database-safe-change", name: "database-safe-change", version: "v3.1", owner_team: "Data Infrastructure",
      kind: "database", status: "active",
      scope: { summary: "Production databases", detail: "RDS · Aurora · Redis", environments: ["production"] },
      safety: { summary: "40% headroom", detail: "Maintenance window" },
      action_path: { method: "scheduled", label: "Scheduled", approval: "DBA approval" },
      verification: { summary: "CloudWatch · 60m", detail: "Manual rollback gate" },
      coverage: { matched: 184, matched_ratio: 0.92, needs_attention: true },
      modified_at: daysAgo(7), modified_by: "data-infra",
    },
    {
      id: "gitops-required-tier1", name: "gitops-required-tier1", version: "v2.0", owner_team: "Security Engineering",
      kind: "gitops", status: "active",
      scope: { summary: "Tier 1 production", detail: "All supported providers", environments: ["production"] },
      safety: { summary: "No direct mutation", detail: "Fresh evidence required" },
      action_path: { method: "gitops_pr", label: "GitOps PR", approval: "Peer approval" },
      verification: { summary: "Source-specific", detail: "Rollback required" },
      coverage: { matched: 96, matched_ratio: 1, needs_attention: false },
      modified_at: daysAgo(14), modified_by: "security-admin",
    },
    {
      id: "storage-lifecycle-governance", name: "storage-lifecycle-governance", version: "v1.2", owner_team: "Data Platform",
      kind: "storage", status: "active",
      scope: { summary: "Object storage", detail: "S3 · GCS", environments: ["production", "datalake"] },
      safety: { summary: "30-day access window", detail: "Retention exclusions" },
      action_path: { method: "terraform_pr", label: "Terraform PR", approval: "Owner approval" },
      verification: { summary: "Inventory · 24h", detail: "Prefix validation" },
      coverage: { matched: 146, matched_ratio: 0.88, needs_attention: true },
      modified_at: daysAgo(14), modified_by: "data-platform",
    },
    {
      id: "llm-routing-cost-guard", name: "llm-routing-cost-guard", version: "v1.0", owner_team: "AI Platform",
      kind: "ai", status: "active",
      scope: { summary: "Production AI gateways", detail: "Bedrock · OpenAI", environments: ["production"] },
      safety: { summary: "P95 latency < 650ms", detail: "Quality floor required" },
      action_path: { method: "gateway_route", label: "Gateway route", approval: "Approval required" },
      verification: { summary: "OTel · 15m", detail: "Quality and cost checks" },
      coverage: { matched: 68, matched_ratio: 1, needs_attention: false },
      modified_at: daysAgo(21), modified_by: "genai-sre",
    },
    {
      id: "cost-anomaly-guard", name: "cost-anomaly-guard", version: "v0.3", owner_team: "FinOps",
      kind: "guardrail", status: "draft",
      scope: { summary: "All environments", detail: "Kubernetes · AWS", environments: ["production", "staging"] },
      safety: { summary: "20% headroom", detail: "Max 10% per cycle" },
      action_path: { method: "gitops_pr", label: "GitOps PR", approval: "Approval required" },
      verification: { summary: "Prometheus · 30m", detail: "Rollback enabled" },
      coverage: { matched: 212, matched_ratio: 0.9, needs_attention: false },
      modified_at: daysAgo(1), modified_by: "finops-lead",
    },
    {
      id: "multi-region-failover-limits", name: "multi-region-failover-limits", version: "v0.1", owner_team: "Reliability",
      kind: "guardrail", status: "draft",
      scope: { summary: "Production", detail: "Multi-region services", environments: ["production"] },
      safety: { summary: "50% headroom", detail: "One region at a time" },
      action_path: { method: "scheduled", label: "Scheduled", approval: "SRE approval" },
      verification: { summary: "Datadog · 45m", detail: "Manual rollback gate" },
      coverage: { matched: 41, matched_ratio: 1, needs_attention: false },
      modified_at: daysAgo(2), modified_by: "sre-oncall",
    },
  ];
}

// Blocked-change counts are windowed; everything else is a current state.
const BLOCKED: Record<string, number> = { P30D: 12, P7D: 3, P1D: 1 };

export function policySummary(policies: Policy[], window: string): PolicySummary {
  return {
    evaluated_at: new Date(Date.now() - 4 * 60_000).toISOString(),
    window,
    active: policies.filter((p) => p.status === "active").length,
    drafts: policies.filter((p) => p.status === "draft").length,
    coverage: policies.length ? { covered: 1294, total: 1420 } : { covered: 0, total: 0 },
    exceptions: policies.length
      ? { open: 4, expiring_soon: 2, expiring_window_days: 7 }
      : { open: 0, expiring_soon: 0, expiring_window_days: 7 },
    blocked_changes: policies.length ? (BLOCKED[window] ?? BLOCKED.P30D) : 0,
    conflicts: 0,
  };
}

// Takes the scenario getter so this file never needs a value import from
// handlers.ts (which would be circular).
export function createPolicyHandlers(getScenario: () => Scenario) {
  return [
    http.get("*/api/v1/policies", ({ request }) => {
      const scenario = getScenario();
      if (scenario === "backend-down" || scenario === "partial-failure") {
        return HttpResponse.json(
          { title: "Policy service unavailable", status: 503, code: "backend_unavailable", retryable: true },
          { status: 503, headers: { "Content-Type": "application/problem+json" } },
        );
      }
      const window = new URL(request.url).searchParams.get("window") ?? "P30D";
      const policies = scenario === "empty-cluster" ? [] : policyFixtures();
      return HttpResponse.json({ policies, summary: policySummary(policies, window) });
    }),
  ];
}