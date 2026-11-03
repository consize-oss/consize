import type { Plugin, PluginDetail, PluginSummary } from "@/schemas";

const ago = (seconds: number) => new Date(Date.now() - seconds * 1000).toISOString();

type Opts = { degradeKubernetes?: boolean };

export function pluginFixtures(opts: Opts = {}): Plugin[] {
  return [
    {
      id: "datadog",
      display_name: "Datadog APM & Metrics",
      capability: "Metrics & SLO evidence",
      health: "healthy",
      last_checked_at: ago(32),
      version: "v3.4.1",
      category: "evidence",
      kind: "evidence",
      source: "registry",
      trust: "verified_publisher",
      description: "Real-time telemetry ingestion, post-action health validation, and SLO evidence.",
      attention: [],
      engine_state: null,
      facts: [
        { label: "Telemetry scope", value: "8 clusters · 614 pods", tone: "default" },
        { label: "Observed signals", value: "Latency, errors, CPU, memory", tone: "default" },
        { label: "Authority", value: "Read-only telemetry", tone: "default" },
      ],
    },
    {
      id: "prometheus",
      display_name: "Prometheus Metrics",
      capability: "Utilization evidence",
      health: "healthy",
      last_checked_at: ago(45),
      version: "v0.3.0",
      category: "evidence",
      kind: "evidence",
      source: "built_in",
      trust: "built_in",
      description: "Scrapes the utilization and restart signals used as evidence for every recommendation.",
      attention: [],
      engine_state: null,
      facts: [
        { label: "Scrape scope", value: "8 clusters", tone: "default" },
        { label: "Observed signals", value: "CPU, memory, restarts", tone: "default" },
        { label: "Authority", value: "Read-only queries", tone: "default" },
      ],
    },
    {
      id: "kubernetes",
      display_name: "Kubernetes Provider",
      capability: "Resource discovery",
      health: opts.degradeKubernetes ? "degraded" : "healthy",
      message: opts.degradeKubernetes ? "API server latency elevated" : undefined,
      last_checked_at: ago(60),
      version: "v0.3.0",
      category: "evidence",
      kind: "evidence",
      source: "built_in",
      trust: "built_in",
      description: "Discovers Deployments with their requests, limits, and rollout state.",
      attention: [],
      engine_state: null,
      facts: [
        { label: "Discovery scope", value: "8 clusters", tone: "default" },
        { label: "Resource kinds", value: "Deployments", tone: "default" },
        { label: "Authority", value: "Read inventory", tone: "default" },
      ],
    },
    {
      id: "kubecost",
      display_name: "Kubecost Allocation",
      capability: "Cost attribution",
      health: "unavailable",
      status_reason: "HTTP 401",
      message: "Credentials were rejected",
      last_checked_at: ago(18 * 60),
      version: "v2.18.0",
      category: "evidence",
      kind: "cost",
      source: "registry",
      trust: "verified_publisher",
      description: "Kubernetes allocation, idle-cost evidence, and workload-level cost attribution.",
      attention: [],
      engine_state: null,
      facts: [
        { label: "Financial scope", value: "4 clusters", tone: "default" },
        { label: "Connection", value: "HTTP 401 · 18m ago", tone: "bad" },
        { label: "Authority", value: "Read allocation API", tone: "default" },
      ],
    },
    {
      id: "aws-discovery",
      display_name: "AWS Discovery",
      capability: "Inventory & pricing",
      health: "healthy",
      last_checked_at: ago(90),
      version: "v1.6.2",
      category: "evidence",
      kind: "evidence",
      source: "registry",
      trust: "verified_publisher",
      description: "Discovers EC2, RDS, S3, and EKS resources and reads provider price lists.",
      attention: ["credential_expiring"],
      engine_state: null,
      facts: [
        { label: "Discovery scope", value: "EC2, RDS, S3, EKS", tone: "default" },
        { label: "Credential", value: "12 days remaining", tone: "warn" },
        { label: "Authority", value: "Read inventory and prices", tone: "default" },
      ],
    },
    {
      id: "github-gitops",
      display_name: "GitHub GitOps Provider",
      capability: "Pull request delivery",
      health: "healthy",
      last_checked_at: ago(180),
      version: "v4.0.2",
      category: "action",
      kind: "action",
      source: "registry",
      trust: "verified_publisher",
      description: "Declarative branches and pull requests with repository and CODEOWNERS checks.",
      attention: ["permission_review"],
      engine_state: null,
      facts: [
        { label: "Repository", value: "acme-corp/k8s-infra-live", tone: "default" },
        { label: "Capabilities", value: "Branch · PR · status", tone: "default" },
        { label: "Package trust", value: "Signature valid", tone: "ok" },
      ],
    },
    {
      id: "kubernetes-apply",
      display_name: "Kubernetes Direct Apply",
      capability: "Dry-run and apply",
      health: "healthy",
      last_checked_at: ago(60),
      version: "v0.3.0",
      category: "action",
      kind: "action",
      source: "built_in",
      trust: "built_in",
      description: "Plans, applies, and rolls back reviewed changes inside permitted namespaces.",
      attention: [],
      engine_state: null,
      facts: [
        { label: "Scope", value: "Namespace-scoped write", tone: "default" },
        { label: "Modes", value: "Dry-run · apply · rollback", tone: "default" },
        { label: "Authority", value: "Requires policy approval", tone: "default" },
      ],
    },
    {
      id: "headroom-p95",
      display_name: "P95 Headroom Rightsizer",
      capability: "Recommendation engine",
      health: "healthy",
      last_checked_at: ago(30),
      version: "v1.0.0",
      category: "algorithm",
      kind: "algorithm",
      source: "built_in",
      trust: "built_in",
      description: "Evaluates usage windows and proposes bounded CPU and memory changes.",
      attention: [],
      engine_state: "active",
      facts: [
        { label: "Window", value: "14-day rolling", tone: "default" },
        { label: "Active proposals", value: "34 recommendations", tone: "ok" },
        { label: "Authority", value: "Advisory only", tone: "default" },
      ],
    },
    {
      id: "slack",
      display_name: "Slack Dispatch",
      capability: "Alert delivery",
      health: "healthy",
      last_checked_at: ago(120),
      version: "v1.2.0",
      category: "notification",
      kind: "notification",
      source: "registry",
      trust: "verified_publisher",
      description: "Posts approval requests and policy blocks to the channels you choose.",
      attention: [],
      engine_state: null,
      facts: [
        { label: "Channel", value: "#finops-alerts", tone: "default" },
        { label: "Events", value: "Approvals · blocks · rollbacks", tone: "default" },
        { label: "Authority", value: "Post messages", tone: "default" },
      ],
    },
  ];
}

// Computed from the fixtures, not hardcoded, so the KPI cards and the list
// can never disagree.
export function pluginSummary(items: Plugin[]): PluginSummary {
  const count = (c: Plugin["category"]) => items.filter((p) => p.category === c).length;
  const evidence = items.filter((p) => p.category === "evidence");
  const active = evidence.filter((p) => p.health !== "unavailable" && p.health !== "setup_required");
  const WINDOW = 120;
  const fresh = active.filter((p) => Date.now() - new Date(p.last_checked_at).getTime() <= WINDOW * 1000);
  const actionPlugins = items.filter((p) => p.category === "action");
  return {
    counts: {
      installed: items.length,
      evidence: count("evidence"),
      algorithm: count("algorithm"),
      action: count("action"),
      notification: count("notification"),
      marketplace: null,
    },
    ingestion: {
      active: active.length,
      total: evidence.length,
      fresh_ratio: active.length ? fresh.length / active.length : null,
      freshness_window_seconds: WINDOW,
    },
    actions: {
      paths: 3,
      summary: "GitOps pull request, direct apply, schedule",
      state:
        actionPlugins.some((p) => p.attention.length > 0 || p.health !== "healthy")
          ? "attention"
          : "ready",
    },
    recommendations: { active: 84, resources: 1420 },
    trust: {
      trusted: items.filter((p) => p.trust !== "unverified").length,
      total: items.length,
    },
  };
}

const EVIDENCE_GUARDRAIL = {
  title: "Verification source",
  description:
    "Supplies the before and after signals used to verify a change and to trigger rollback when a threshold is breached.",
};

const DETAIL_EXTRAS: Record<string, Partial<PluginDetail>> = {
  datadog: {
    overview:
      "Primary telemetry source for operational evidence and post-action verification across production workloads.",
    scope_stats: [
      { label: "Target fleet", value: "8 clusters", sub: "614 pods tracked", tone: "default" },
      { label: "Ingestion", value: "12,410 metrics/s", sub: "Synced 32s ago", tone: "ok" },
    ],
    guardrail: {
      title: "Canary health policy",
      description:
        "30-minute observation window. Triggers rollback when configured error or latency thresholds are breached.",
    },
    credentials: {
      state: "encrypted",
      entries: [
        { label: "API key", masked: "••••••••8f2a" },
        { label: "App key", masked: "••••••••99c1" },
      ],
    },
  },
  prometheus: { guardrail: EVIDENCE_GUARDRAIL },
  kubecost: {
    overview:
      "Supplies workload-level cost attribution. Its connection is failing, so cost evidence is stale until the credential is replaced.",
    credentials: { state: "invalid", entries: [{ label: "API token", masked: "••••••••c04e" }] },
  },
  "aws-discovery": {
    credentials: { state: "expiring", entries: [{ label: "Role", masked: "••••••••AssumeRole" }] },
  },
  "github-gitops": {
    overview:
      "Creates reviewable pull requests for approved changes. A newer version asks for expanded repository permissions and needs review before it is applied.",
    credentials: {
      state: "encrypted",
      entries: [
        { label: "App ID", masked: "••••••••3181" },
        { label: "Private key", masked: "••••••••e9d2" },
      ],
    },
  },
  slack: {
    credentials: { state: "encrypted", entries: [{ label: "Webhook", masked: "••••••••t9QZ" }] },
  },
};

export function pluginDetailFor(id: string, opts: Opts = {}): PluginDetail | undefined {
  const p = pluginFixtures(opts).find((x) => x.id === id);
  if (!p) return undefined;
  return {
    ...p,
    overview: p.description,
    scope_stats: p.facts.slice(0, 2).map((f) => ({ label: f.label, value: f.value, tone: f.tone })),
    guardrail: null,
    credentials: null,
    ...DETAIL_EXTRAS[id],
  };
}