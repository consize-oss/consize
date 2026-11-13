import { z } from "zod";
import type { components } from "../../lib/generated/api";
import { apiGet, ApiError } from "./api-client";
import { resourceListSchema } from "@/features/resources/queries";
import type {
  AuditEvent,
  DashboardSummary,
  RecommendationListItem,
  RecommendationDetail,
  Plugin,
  PluginSummary,
} from "@/schemas";

type Api = components["schemas"];
const object = z.record(z.string(), z.unknown());
const strings = z
  .array(z.string())
  .nullable()
  .transform((x) => x ?? []);
const evidence = z.object({
  id: z.string(),
  signal: z.string(),
  value: z.number().nullable(),
  unit: z.string(),
  aggregation: z.string(),
  window_start: z.string(),
  window_end: z.string(),
  fresh_until: z.string(),
  quality: z.enum([
    "valid",
    "missing",
    "stale",
    "partial",
    "invalid",
    "unavailable",
  ]),
  plugin_id: z.string(),
});
export const runtimeRecommendationSchema = z.object({
  id: z.number().int().positive(),
  resource_id: z.string(),
  title: z.string(),
  summary: z.string(),
  algorithm_id: z.string(),
  algorithm_version: z.string().optional(),
  current: object,
  proposed: object,
  parameters: object,
  confidence: z.enum(["low", "medium", "high"]),
  risk: z.enum(["low", "medium", "high"]),
  status: z.string(),
  policy_id: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  superseded_by: z.number().optional(),
  evidence_snapshot: z.array(evidence).optional(),
  cost_estimate: z
    .object({
      currency: z.string(),
      savings_monthly: z.number(),
      pricing_source: z.string(),
    })
    .optional(),
});
export const runtimePluginSchema = z.object({
  manifest: z.object({
    id: z.string(),
    display_name: z.string(),
    version: z.string(),
    category: z.string(),
    supported_resource_types: strings,
    supported_action_types: strings,
    capabilities: strings,
    can_mutate_infrastructure: z.boolean(),
    requires_approval: z.boolean(),
  }),
  health: z.object({
    status: z.string(),
    message: z.string(),
    checked_at: z.string(),
  }),
}) satisfies z.ZodType<Api["PluginStatus"]>;
export const runtimeJobSchema = z.object({
  id: z.number(),
  recommendation_id: z.number().optional(),
  action_id: z.number().optional(),
  state: z.string(),
  window_start: z.string(),
  deadline: z.string(),
  last_error: z.string().optional(),
  updated_at: z.string(),
  verification_result: z
    .object({ status: z.string(), reasons: strings })
    .optional(),
});
export type RuntimeJob = z.infer<typeof runtimeJobSchema>;
export const terminalJobs = new Set([
  "verified",
  "rolled_back",
  "failed",
  "cancelled",
  "manual_intervention",
]);
export const runtimePlanSchema = z.object({
  planned: z.object({
    plan: z.object({
      summary: z.string(),
      original_state: object.optional(),
      applied_state: object.optional(),
      preflight: z
        .array(
          z.object({ id: z.string(), status: z.string(), message: z.string() }),
        )
        .optional(),
      requires_approval: z.boolean(),
    }),
    policy_decision: z.object({ decision: z.string(), reasons: strings }),
  }),
});
export function canApprovePlan(
  plan: z.infer<typeof runtimePlanSchema>["planned"] | undefined,
): boolean {
  return Boolean(
    plan &&
      [
        "recommend_only",
        "approval_required",
        "approved",
        "auto_apply",
      ].includes(plan.policy_decision.decision) &&
      plan.plan.preflight?.length &&
      plan.plan.preflight.every((check) => check.status === "passed"),
  );
}
export const runtimeAcceptedSchema = z.object({ job: runtimeJobSchema });
export const runtimeDashboardSchema = z.object({
  resources: resourceListSchema.shape.resources
    .nullable()
    .transform((x) => x ?? []),
  recommendations: z
    .array(runtimeRecommendationSchema)
    .nullable()
    .transform((x) => x ?? []),
  jobs: z
    .array(runtimeJobSchema)
    .nullable()
    .transform((x) => x ?? []),
  plugins: z
    .array(runtimePluginSchema)
    .nullable()
    .transform((x) => x ?? []),
  policy: z.object({
    id: z.string(),
    name: z.string(),
    mode: z.string(),
    verification: z.string(),
    approval_model: z.string(),
  }),
  verification: z.object({
    enabled: z.boolean(),
    metrics_plugin_id: z.string(),
  }),
});
export type RuntimeDashboard = z.infer<typeof runtimeDashboardSchema>;
export const readDashboard = (signal?: AbortSignal) =>
  apiGet("/api/dashboard", runtimeDashboardSchema, signal);
export const unavailableMoney = {
  amount_minor: 0,
  currency: "",
  period: "month" as const,
  basis: "unavailable" as const,
};

export function recommendationView(
  rec: RuntimeDashboard["recommendations"][number],
  data: RuntimeDashboard,
): RecommendationListItem {
  const resource = data.resources.find((x) => x.id === rec.resource_id);
  if (!resource)
    throw new ApiError(-1, {
      title: "Recommendation resource is absent from inventory",
      status: -1,
    });
  const job = data.jobs.find((x) => x.recommendation_id === rec.id);
  const status: RecommendationListItem["status"] =
    rec.status === "superseded"
      ? "superseded"
      : job
        ? ["verified", "rolled_back"].includes(job.state)
          ? "executed"
          : terminalJobs.has(job.state)
            ? "blocked"
            : "planned"
        : ["pending", "planned", "approved"].includes(rec.status)
          ? "pending_approval"
          : "blocked";
  return {
    id: String(rec.id),
    version: rec.updated_at,
    title: rec.title,
    target_summary: rec.summary,
    resource: {
      id: resource.id,
      name: resource.name,
      provider: resource.provider,
      environment: resource.environment,
      type: resource.type,
      source: resource.source_plugin_id,
      region: resource.region,
      namespace:
        typeof resource.metadata.namespace === "string"
          ? resource.metadata.namespace
          : undefined,
      tags: resource.labels,
    },
    owner: resource.owner,
    policy_id: rec.policy_id || data.policy.id,
    workflow_state: job?.state ?? rec.status,
    confidence: rec.confidence,
    risk: rec.risk,
    status,
    estimated_savings: rec.cost_estimate?.currency
      ? {
          amount_minor: Math.round(rec.cost_estimate.savings_monthly * 100),
          currency: rec.cost_estimate.currency,
          period: "month",
          basis: "estimated",
        }
      : unavailableMoney,
    available_remediations: [
      { method: "review", label: "Review", enabled: true },
    ],
  };
}
export function recommendationViews(
  data: RuntimeDashboard,
  filters: Record<string, string | undefined> = {},
) {
  const items = data.recommendations
    .map((x) => recommendationView(x, data))
    .filter((x) => {
      const fields: Record<string, string> = {
        provider: x.resource.provider,
        source: x.resource.source,
        region: x.resource.region ?? "",
        type: x.resource.type,
        environment: x.resource.environment,
        owner: x.owner,
        risk: x.risk,
        status: x.status,
      };
      return (
        Object.entries(fields).every(
          ([key, value]) => !filters[key] || filters[key] === value,
        ) &&
        (!filters.q ||
          [
            x.title,
            x.resource.name,
            x.resource.id,
            x.owner,
            ...Object.values(x.resource.tags ?? {}),
          ]
            .join(" ")
            .toLowerCase()
            .includes(filters.q.toLowerCase()))
      );
    });
  return { items, page: { has_more: false, next_cursor: null } };
}
export function summaryView(
  data: RuntimeDashboard,
  filters: Record<string, string | undefined>,
): DashboardSummary {
  const recs = recommendationViews(data, filters).items;
  const verified = data.jobs.filter((x) => x.state === "verified");
  const rolled = data.jobs.filter((x) => x.state === "rolled_back");
  return {
    scope: {
      provider: filters.provider ?? null,
      source: filters.source ?? null,
      region: filters.region ?? null,
      type: filters.type ?? null,
    },
    window: filters.window ?? "P30D",
    projected_savings: unavailableMoney,
    verified_operational_savings: unavailableMoney,
    pending_approvals_savings: unavailableMoney,
    projected_change_ratio: null,
    verified_changes: verified.length,
    pending_approvals: recs.filter((x) => x.status === "pending_approval")
      .length,
    pending_approvals_mine: null,
    rollback_rate: data.jobs.length ? rolled.length / data.jobs.length : 0,
    rollbacks: rolled.length,
    guarded_changes: data.jobs.length,
    open_recommendations: recs.filter((x) => x.status === "pending_approval")
      .length,
    policy_engine: "unknown",
    verifier: data.verification.enabled ? "unknown" : "unavailable",
    last_evaluated_at:
      data.recommendations
        .map((x) => x.created_at)
        .sort()
        .at(-1) ?? "",
    unscoped_metrics: ["verified_operational_savings", "rollback_rate"],
    generated_at: new Date().toISOString(),
    freshness: "fresh",
  };
}
export function pluginView(p: RuntimeDashboard["plugins"][number]): Plugin {
  const m = p.manifest;
  const category: Plugin["category"] =
    m.category === "metrics" ||
    m.category === "cost" ||
    m.category === "data_source"
      ? "evidence"
      : m.category === "algorithm"
        ? "algorithm"
        : m.category === "action"
          ? "action"
          : "notification";
  return {
    id: m.id,
    display_name: m.display_name,
    version: m.version,
    category,
    kind: m.category === "cost" ? "cost" : category,
    capability: m.capabilities.join(", "),
    health:
      p.health.status === "healthy"
        ? "healthy"
        : p.health.status === "degraded"
          ? "degraded"
          : "unavailable",
    message: p.health.message,
    last_checked_at: p.health.checked_at,
    source: "unknown",
    trust: "unverified",
    description: p.health.message,
    attention: [],
    engine_state: null,
    facts: [
      {
        label: "Capabilities",
        value: m.capabilities.join(", ") || "None reported",
        tone: "default",
      },
      {
        label: "Resources",
        value: m.supported_resource_types.join(", ") || "None reported",
        tone: "default",
      },
      {
        label: "Approval",
        value: m.requires_approval ? "Required" : "Not required by plugin",
        tone: "default",
      },
    ],
  };
}
export function pluginsView(data: RuntimeDashboard) {
  const items = data.plugins.map(pluginView);
  const ingestion = items.filter((x) => x.category === "evidence");
  const actions = items.filter((x) => x.category === "action");
  const summary: PluginSummary = {
    counts: {
      installed: items.length,
      evidence: ingestion.length,
      algorithm: items.filter((x) => x.category === "algorithm").length,
      action: actions.length,
      notification: items.filter((x) => x.category === "notification").length,
      marketplace: null,
    },
    ingestion: {
      active: ingestion.filter((x) => x.health === "healthy").length,
      total: ingestion.length,
      fresh_ratio: null,
      freshness_window_seconds: 0,
    },
    actions: {
      paths: actions.length,
      summary:
        "Registered action providers; execution still requires policy and preflight checks",
      state:
        actions.length > 0 && actions.every((x) => x.health === "healthy")
          ? "ready"
          : "attention",
    },
    recommendations: {
      active: data.recommendations.filter((x) => x.status === "pending").length,
      resources: new Set(data.recommendations.map((x) => x.resource_id)).size,
    },
    trust: { trusted: 0, total: items.length, assessed: false },
  };
  return { items, summary };
}
export const actionEventsSchema = z.object({
  actions: z
    .array(
      z.object({
        id: z.number(),
        action_id: z.number().optional(),
        recommendation_id: z.number().optional(),
        resource_id: z.string(),
        actor: z.string(),
        result: z.string(),
        message: z.string(),
        created_at: z.string(),
      }),
    )
    .nullable()
    .transform((x) => x ?? []),
});
export function auditView(
  e: z.infer<typeof actionEventsSchema>["actions"][number],
): AuditEvent {
  const outcomes: Record<string, AuditEvent["outcome"]> = {
    verified: "verified",
    rolled_back: "rolled_back",
    verifying: "verifying",
    rollback_verifying: "verifying",
    manual_intervention: "manual_intervention",
    failed: "failed",
    cancelled: "blocked",
    waiting_rollout: "applied",
  };
  return {
    id: String(e.id),
    title: e.message,
    actor: e.actor,
    correlation_id: e.action_id ? `action:${e.action_id}` : `event:${e.id}`,
    occurred_at: e.created_at,
    outcome: outcomes[e.result] ?? "pending",
  };
}
export async function detailView(
  id: string,
  signal?: AbortSignal,
): Promise<RecommendationDetail> {
  const [data, events] = await Promise.all([
    readDashboard(signal),
    apiGet("/api/actions", actionEventsSchema, signal),
  ]);
  const rec = data.recommendations.find((x) => String(x.id) === id);
  if (!rec)
    throw new ApiError(404, { title: "Recommendation not found", status: 404 });
  const assessment = z
    .object({
      observed_history: z.string(),
      required_history: z.string(),
      coverage_ratio: z.number(),
      reasons: strings,
    })
    .safeParse(rec.parameters.confidence_assessment);
  const asText = (x: Record<string, unknown>) =>
    Object.fromEntries(
      Object.entries(x).map(([k, v]) => [
        k,
        typeof v === "string" ? v : JSON.stringify(v),
      ]),
    );
  return {
    ...recommendationView(rec, data),
    summary: rec.summary,
    algorithm: {
      id: rec.algorithm_id,
      version: rec.algorithm_version ?? "Unavailable",
    },
    current: asText(rec.current),
    proposed: asText(rec.proposed),
    confidence_assessment: assessment.success
      ? assessment.data
      : {
          observed_history: "Unavailable",
          required_history: "Unavailable",
          coverage_ratio: 0,
          reasons: ["No confidence assessment supplied"],
        },
    evidence: (rec.evidence_snapshot ?? []).map((e) => ({
      signal: e.signal,
      value: e.value,
      unit: e.unit,
      aggregation: e.aggregation,
      window: `${e.window_start} – ${e.window_end}`,
      source: e.plugin_id,
      freshness:
        e.quality === "missing" || e.quality === "unavailable"
          ? "unavailable"
          : Date.parse(e.fresh_until) > Date.now()
            ? "fresh"
            : "stale",
      quality:
        e.quality === "stale"
          ? "partial"
          : e.quality === "unavailable"
            ? "missing"
            : e.quality,
      reason_codes: [e.quality],
    })),
    policy: {
      decision: "requires_approval",
      reasons: ["Run a dry-run plan to evaluate current policy and preflight."],
      verification: [data.policy.verification],
      required_approval: data.policy.approval_model,
    },
    superseded_by: rec.superseded_by ? String(rec.superseded_by) : null,
    workload_classification: null,
    audit: events.actions
      .filter((x) => x.recommendation_id === rec.id)
      .map(auditView),
  };
}
