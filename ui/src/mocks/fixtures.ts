import type {
  AuditEvent,
  DashboardSummary,
  Money,
  RecommendationDetail,
  RecommendationListItem,
  ScopeFilter,
  ScopeOptions,
} from "@/schemas";

export const money = (
  amount_minor: number,
  basis: Money["basis"] = "estimated",
): Money => ({ amount_minor, currency: "USD", period: "month", basis });

// Order = the backend's priority ranking (the Overview shows the top 5).
// rec_04 is superseded: it stays inspectable but never appears in the ranking.
export const baseRecommendations: RecommendationListItem[] = [
  {
    id: "rec_01",
    version: "7",
    title: "Right-size payment-service-deployment",
    resource: {
      id: "res_01",
      name: "payment-service-deployment",
      provider: "kubernetes",
      environment: "production",
      namespace: "payments",
      region: "us-east-1",
      type: "Deployment",
      source: "prod-us-east-1",
    },
    
    target_summary: "c6i.2xlarge · 8→4 vCPU",
    owner: "Platform",
    policy_id: "prod-conservative",
    risk: "low",
    confidence: "high",
    estimated_savings: money(420000),
    status: "actionable",
    available_remediations: [
      { method: "review", label: "Review change", enabled: true },
      {
        method: "github_pull_request",
        label: "Review & create PR",
        enabled: true,
        plugin_instance_id: "github-gitops:infra",
      },
      {
        method: "direct_apply",
        label: "Apply directly",
        enabled: false,
        disabled_reason: "policy_requires_pull_request",
      },
    ],
  },
  {
    id: "rec_02",
    version: "3",
    title: "Downscale orders-aurora-cluster",
    resource: {
      id: "res_02",
      name: "orders-aurora-cluster",
      provider: "aws",
      environment: "production",
      region: "us-east-1",
      type: "Aurora cluster",
      source: "acme-prod-aws",
    },
    target_summary: "db.r6g.4xlarge → 2xlarge",
    owner: "Checkout",
    policy_id: "db-safe-change",
    risk: "medium",
    confidence: "medium",
    estimated_savings: money(1280000),
    status: "pending_approval",
    available_remediations: [
      { method: "review", label: "Review change", enabled: true },
      { method: "schedule", label: "Schedule", enabled: true },
    ],
  },
  {
    id: "rec_05",
    version: "2",
    title: "Move analytics-raw-events to Glacier Instant Retrieval",
    resource: {
      id: "res_05",
      name: "analytics-raw-events",
      provider: "aws",
      environment: "production",
      region: "us-east-1",
      type: "S3 bucket",
      source: "acme-prod-aws",
    },
    target_summary: "S3 Standard → Glacier IR",
    owner: "Data Eng",
    policy_id: "lifecycle",
    risk: "low",
    confidence: "high",
    estimated_savings: money(195000),
    status: "actionable",
    available_remediations: [
      { method: "review", label: "Review change", enabled: true },
      { method: "direct_apply", label: "Apply change", enabled: true },
    ],
  },
  {
    id: "rec_03",
    version: "2",
    title: "Right-size ml-infer-cluster",
    resource: {
      id: "res_03",
      name: "ml-infer-cluster",
      provider: "kubernetes",
      environment: "production",
      namespace: "ml-ops",
      region: "us-east-1",
      type: "GPU node group",
      source: "prod-us-east-1",
    },
    target_summary: "g5.4xlarge · 4→3 nodes",
    owner: "ML Ops",
    policy_id: "gpu-strict",
    risk: "high",
    confidence: "low",
    estimated_savings: money(840000),
    status: "blocked",
    available_remediations: [
      {
        method: "direct_apply",
        label: "Apply directly",
        enabled: false,
        disabled_reason: "approval_required",
      },
    ],
  },
  {
    id: "rec_06",
    version: "4",
    title: "Right-size staging-worker-pool",
    resource: {
      id: "res_06",
      name: "staging-worker-pool",
      provider: "kubernetes",
      environment: "staging",
      namespace: "workers",
      region: "us-west-2",
      type: "Worker pool",
      source: "staging-us-west-2",
    },
    target_summary: "Non-prod limits · 8→4 nodes",
    owner: "Infra Core",
    policy_id: "non-prod-auto",
    risk: "low",
    confidence: "high",
    estimated_savings: money(264000),
    status: "actionable",
    available_remediations: [
      { method: "review", label: "Review change", enabled: true },
      { method: "direct_apply", label: "Direct apply", enabled: true },
    ],
  },
  {
    id: "rec_04",
    version: "1",
    title: "Right-size payment-service-deployment (earlier run)",
    resource: {
      id: "res_01",
      name: "payment-service-deployment",
      provider: "kubernetes",
      environment: "production",
      namespace: "payments",
      region: "us-east-1",
      type: "Deployment",
      source: "prod-us-east-1",
    },
    target_summary: "c6i.2xlarge · 8→6 vCPU",
    owner: "Platform",
    policy_id: "prod-conservative",
    risk: "low",
    confidence: "medium",
    estimated_savings: money(380000),
    status: "superseded",
    available_remediations: [
      { method: "review", label: "Review change", enabled: false, disabled_reason: "superseded" },
    ],
  },
];

// --- scope ---------------------------------------------------------------
export function matchesScope(r: RecommendationListItem, s: Partial<ScopeFilter>) {
  return (
    (!s.provider || r.resource.provider === s.provider) &&
    (!s.source || r.resource.source === s.source) &&
    (!s.region || r.resource.region === s.region) &&
    (!s.type || r.resource.type === s.type)
  );
}

const PROVIDER_LABELS: Record<string, string> = { kubernetes: "Kubernetes", aws: "AWS" };
const unique = (xs: (string | undefined)[]) =>
  Array.from(new Set(xs.filter((x): x is string => Boolean(x)))).sort();

// "Options are populated from connected sources and discovered resources."
export function buildScopeOptions(): ScopeOptions {
  const rs = baseRecommendations.map((r) => r.resource);
  const opts = (xs: string[], label = (x: string) => x) =>
    xs.map((value) => ({ value, label: label(value) }));
  return {
    providers: opts(unique(rs.map((r) => r.provider)), (v) => PROVIDER_LABELS[v] ?? v),
    sources: opts(unique(rs.map((r) => r.source))),
    regions: opts(unique(rs.map((r) => r.region))),
    resource_types: opts(unique(rs.map((r) => r.type))),
  };
}

// --- summary -------------------------------------------------------------
// Verified savings and rollback numbers are *windowed* fleet figures.
// Projected savings is a current estimate, so it is not windowed.
const WINDOWED = {
  P30D: { verified: 9240000, changes: 34, guarded: 142, ratio: 0.142 },
  P7D: { verified: 1840000, changes: 9, guarded: 31, ratio: 0.031 },
  P1D: { verified: 260000, changes: 2, guarded: 5, ratio: 0.004 },
} as const;

export function buildSummary(
  scope: Partial<ScopeFilter>,
  window: string,
  opts: { stale: boolean; degraded: boolean; empty: boolean },
): DashboardSummary {
  const w = WINDOWED[window as keyof typeof WINDOWED] ?? WINDOWED.P30D;
  const scoped = Boolean(scope.provider || scope.source || scope.region || scope.type);
  const open = baseRecommendations.filter((r) => r.status !== "superseded" && matchesScope(r, scope));
  const pending = open.filter((r) => r.status === "pending_approval");
  const sum = (xs: RecommendationListItem[]) =>
    xs.reduce((n, r) => n + r.estimated_savings.amount_minor, 0);

  const empty = opts.empty;
  return {
    scope: {
      provider: scope.provider ?? null,
      source: scope.source ?? null,
      region: scope.region ?? null,
      type: scope.type ?? null,
    },
    window,
    // Fleet figures mimic a large estate; a scoped view is derived from the
    // fixtures so changing scope visibly changes what is counted.
    projected_savings: money(empty ? 0 : scoped ? sum(open) : 14825000),
    projected_change_ratio: empty || scoped ? null : w.ratio,
    verified_operational_savings: money(empty ? 0 : w.verified, "verified_operational"),
    verified_changes: empty ? 0 : w.changes,
    pending_approvals: empty ? 0 : scoped ? pending.length : 7,
    pending_approvals_mine: empty ? 0 : scoped ? Math.min(3, pending.length) : 3,
    pending_approvals_savings: money(empty ? 0 : scoped ? sum(pending) : 2810000),
    rollback_rate: 0,
    rollbacks: 0,
    guarded_changes: empty ? 0 : w.guarded,
    open_recommendations: empty ? 0 : scoped ? open.length : 84,
    policy_engine: opts.degraded ? "unavailable" : "enforcing",
    verifier: opts.degraded ? "degraded" : "healthy",
    last_evaluated_at: new Date(Date.now() - 4 * 60_000).toISOString(),
    unscoped_metrics: scoped ? ["verified_operational_savings", "rollback_rate"] : [],
    generated_at: new Date().toISOString(),
    freshness: opts.stale ? "stale" : "fresh",
  };
}

// --- audit -----------------------------------------------------------------
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

export const auditEvents: AuditEvent[] = [
  {
    id: "evt_01",
    title: "Optimization applied: staging-worker-pool scaled down by 4 nodes",
    highlight: "staging-worker-pool",
    actor: "auto-remediate-daemon",
    policy_id: "non-prod-auto",
    signal: "-$1,800/mo impact",
    correlation_id: "corr_8a21f",
    occurred_at: ago(12),
    outcome: "applied",
  },
  {
    id: "evt_02",
    title: "GitOps PR generated: #1402 orders-aurora-cluster instance right-sizing",
    highlight: "#1402 orders-aurora-cluster",
    actor: "FinOps Bot",
    branch: "finops/aurora-2xlarge-downscale",
    note: "Awaiting SRE signoff",
    correlation_id: "corr_44d09",
    occurred_at: ago(60),
    outcome: "awaiting_approval",
  },
  {
    id: "evt_03",
    title: "Verification passed: checkout-gateway latency & CPU headroom preserved",
    highlight: "checkout-gateway",
    actor: "verification-worker",
    signal: "P99 latency 42ms (threshold <65ms)",
    note: "Zero errors over the observation window",
    correlation_id: "corr_1f930",
    occurred_at: ago(180),
    outcome: "verified",
  },
  {
    id: "evt_04",
    title: "Policy blocked: Direct apply on payment-db denied",
    highlight: "payment-db",
    actor: "policy-guard",
    policy_id: "production-conservative",
    signal: "Violation: direct mutation disallowed for tier-1 prod database",
    correlation_id: "corr_55c2e",
    occurred_at: ago(300),
    outcome: "blocked",
  },
];

// --- detail ----------------------------------------------------------------
function buildDetail(
  rec: RecommendationListItem,
  extra: Partial<RecommendationDetail> = {},
): RecommendationDetail {
  return {
    ...rec,
    summary: `Requests on ${rec.resource.name} are well above observed peak usage. Lowering them keeps headroom over the 14-day P95 while reducing reserved capacity.`,
    algorithm: { id: "headroom-p95", version: "1.0.0" },
    current: { "CPU request": "8 vCPU", "Memory request": "16 GiB" },
    proposed: { "CPU request": "4 vCPU", "Memory request": "8 GiB" },
    confidence_assessment: {
      observed_history: "14 days",
      required_history: "14 days",
      coverage_ratio: 0.98,
      reasons: ["Full observation window covered", "No gaps longer than 15 minutes"],
    },
    evidence: [
      {
        signal: "CPU usage",
        value: 2.2,
        unit: "vCPU",
        aggregation: "p95",
        window: "14d",
        source: "Prometheus",
        freshness: "fresh",
        quality: "valid",
        reason_codes: [],
      },
      {
        signal: "Memory working set",
        value: 4.6,
        unit: "GiB",
        aggregation: "p95",
        window: "14d",
        source: "Prometheus",
        freshness: "fresh",
        quality: "valid",
        reason_codes: [],
      },
    ],
    policy: {
      decision: "requires_approval",
      reasons: ["Production workloads require peer approval"],
      required_approval: "SRE",
      verification: ["Error rate must not exceed baseline", "P99 latency must not regress"],
    },
    superseded_by: null,
    workload_classification: null,
    audit: [
      {
        id: "evt_a1",
        title: "Recommendation generated",
        actor: "headroom-p95 v1.0.0",
        correlation_id: "corr_a1b2c",
        occurred_at: ago(26 * 60),
        outcome: "applied",
      },
    ],
    ...extra,
  };
}

const detailOverrides: Record<string, Partial<RecommendationDetail>> = {
  rec_02: {
    current: { "Instance class": "db.r6g.4xlarge" },
    proposed: { "Instance class": "db.r6g.2xlarge" },
    evidence: [
      {
        signal: "CPU usage",
        value: 31,
        unit: "%",
        aggregation: "p95",
        window: "14d",
        source: "Prometheus",
        freshness: "stale",
        quality: "partial",
        reason_codes: ["gap_detected", "coverage_below_target"],
      },
    ],
    confidence_assessment: {
      observed_history: "9 days",
      required_history: "14 days",
      coverage_ratio: 0.64,
      reasons: ["Only 9 of 14 days observed", "Gap during last weekend"],
    },
    policy: {
      decision: "requires_approval",
      reasons: ["Database changes require DBA approval"],
      required_approval: "DBA",
      verification: ["Replica lag must stay under threshold"],
    },
  },
  rec_03: {
    current: { "GPU nodes": "4 x g5.4xlarge" },
    proposed: { "GPU nodes": "3 x g5.4xlarge" },
    evidence: [
      {
        signal: "GPU utilization",
        value: null, // genuinely no data — must render as "No data", never 0
        unit: "%",
        aggregation: "p95",
        window: "14d",
        source: "Prometheus",
        freshness: "unavailable",
        quality: "missing",
        reason_codes: ["metric_not_exported"],
      },
    ],
    confidence_assessment: {
      observed_history: "0 days",
      required_history: "14 days",
      coverage_ratio: 0,
      reasons: ["GPU utilization metric is not being exported"],
    },
    policy: {
      decision: "blocked",
      reasons: ["gpu-strict requires valid GPU utilization evidence"],
      required_approval: "ML platform lead",
      verification: ["Inference latency must not regress"],
    },
  },
  rec_05: {
    current: { "Storage class": "S3 Standard" },
    proposed: { "Storage class": "Glacier Instant Retrieval" },
    evidence: [
      {
        signal: "Objects not read",
        value: 94,
        unit: "%",
        aggregation: "share",
        window: "90d",
        source: "Access logs",
        freshness: "fresh",
        quality: "valid",
        reason_codes: [],
      },
    ],
    policy: {
      decision: "allowed",
      reasons: ["Lifecycle changes are reversible and non-destructive"],
      verification: ["Retrieval requests must keep succeeding"],
    },
  },
  rec_04: { superseded_by: "rec_01" },
};

export function detailFor(id: string): RecommendationDetail | undefined {
  const rec = baseRecommendations.find((r) => r.id === id);
  return rec ? buildDetail(rec, detailOverrides[id]) : undefined;
}