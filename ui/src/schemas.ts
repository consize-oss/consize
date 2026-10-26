import { z } from "zod";

// -- Section 6.5 Money --------------------------------------------------
export const moneySchema = z.object({
  amount_minor: z.number(),
  currency: z.string(),
  period: z.literal("month"),
  basis: z.enum(["estimated", "verified_operational", "billing_realized"]),
});
export type Money = z.infer<typeof moneySchema>;

// -- Section 6.6 Pagination envelope -------------------------------------
export const pageEnvelope = <T extends z.ZodTypeAny>(item: T) =>
  z.object({
    items: z.array(item),
    page: z.object({
      next_cursor: z.string().nullable(),
      has_more: z.boolean(),
    }),
  });

// -- Section 7 Problem Details (RFC 9457) --------------------------------
export const problemSchema = z.object({
  type: z.string().optional(),
  title: z.string(),
  status: z.number(),
  detail: z.string().optional(),
  code: z.string().optional(),
  request_id: z.string().optional(),
  retryable: z.boolean().optional(),
  violations: z
    .array(z.object({ field: z.string(), reason: z.string(), message: z.string() }))
    .optional(),
});

// -- Section 11 Remediation methods ---------------------------------------
export const remediationSchema = z.object({
  method: z.enum(["review", "direct_apply", "github_pull_request", "schedule", "recover"]),
  label: z.string(),
  enabled: z.boolean(),
  disabled_reason: z.string().optional(),
  plugin_instance_id: z.string().optional(),
});

// -- Section 8.3 Recommendation (list shape) ------------------------------
export const riskSchema = z.enum(["low", "medium", "high"]);
export const confidenceSchema = z.enum(["low", "medium", "high"]);

export const recommendationListItemSchema = z.object({
  id: z.string(),
  version: z.string(),
  title: z.string(),
  resource: z.object({
    id: z.string(),
    name: z.string(),
    provider: z.string(),
    environment: z.string(),
    namespace: z.string().optional(),
    // Optional searchable metadata used by the frontend mock; confirm with the backend.
    tags: z.record(z.string()).optional(),
    region: z.string().optional(),
    // type + source drive the table icon and the infrastructure-scope filter.
    type: z.string(),
    source: z.string(),
    // Optional fields for the new list design; older API responses still work.
    kind: z.enum(["compute", "database", "storage", "ai", "gpu"]).optional(),
    description: z.string().optional(),
    scope: z.string().optional(),
    inspect: z.boolean().optional(),
  }),
  // Short, backend-written description of the proposed target (PRD §7.5).
  target_summary: z.string(),
  owner: z.string(),
  policy_id: z.string(),
  risk: riskSchema,
  confidence: confidenceSchema,
  // New mock fields: confirm these names with the backend before integration.
  // The score is supplied independently from the legacy confidence category.
  confidence_percentage: z.number().min(0).max(100).nullable().optional(),
  confidence_note: z.string().optional(),
  change_summary: z.object({ current: z.string(), proposed: z.string() }).optional(),
  savings_note: z.string().optional(),
  estimated_savings: moneySchema,
  status: z.enum(["actionable", "blocked", "pending_approval", "planned", "executed", "superseded"]),
  available_remediations: z.array(remediationSchema),
});
export type RecommendationListItem = z.infer<typeof recommendationListItemSchema>;

// The demo supplies evaluated_at; agree this field with the real backend at integration.
// Keep it optional so an older response without it still loads (the header shows an em dash).
export const recommendationListSchema = pageEnvelope(recommendationListItemSchema).extend({
  evaluated_at: z.string().datetime().nullable().optional(),
});

// -- Section 8.1 Dashboard summary ----------------------------------------
export const scopeSchema = z.object({
  provider: z.string().nullable(),
  source: z.string().nullable(),
  region: z.string().nullable(),
  type: z.string().nullable(),
});
export type ScopeFilter = z.infer<typeof scopeSchema>;

export const dashboardSummarySchema = z.object({
  scope: scopeSchema,
  window: z.string(),
  projected_savings: moneySchema,
  // vs. the previous equal-length period; null when the backend can't compare.
  projected_change_ratio: z.number().nullable(),
  verified_operational_savings: moneySchema,
  verified_changes: z.number(),
  pending_approvals: z.number(),
  pending_approvals_mine: z.number(),
  pending_approvals_savings: moneySchema,
  // PRD §7.3: rollback rate shows numerator, denominator and window.
  rollback_rate: z.number(),
  rollbacks: z.number(),
  guarded_changes: z.number(),
  open_recommendations: z.number(),
  // PRD §7.2/§7.8: these labels may only render when the backend says so.
  policy_engine: z.enum(["enforcing", "unavailable"]),
  verifier: z.enum(["healthy", "degraded", "unavailable"]),
  last_evaluated_at: z.string(),
  // Metrics the backend did NOT filter by the requested scope. The UI must
  // say so instead of implying they were (ticket: "state clearly when a view
  // is not filterable").
  unscoped_metrics: z.array(z.string()),
  generated_at: z.string(),
  freshness: z.enum(["fresh", "stale", "unavailable"]),
});
export type DashboardSummary = z.infer<typeof dashboardSummarySchema>;

// -- Section 8.4 Plugins / capabilities ------------------------------------
// PRD §7.4: "The current alpha should normally show Kubernetes and
// Prometheus capabilities" — the schema stays generic, the mock data is
// what's scoped down, not the contract.
export const capabilitySchema = z.object({
  id: z.string(),
  display_name: z.string(),
  capability: z.string(),
  health: z.enum(["healthy", "degraded", "unavailable", "setup_required"]),
  message: z.string().optional(),
  last_checked_at: z.string(),
});
export const capabilityListSchema = z.object({ items: z.array(capabilitySchema) });
export type Capability = z.infer<typeof capabilitySchema>;

// -- Section 7.8 Audit events ----------------------------------------------
export const auditEventSchema = z.object({
  id: z.string(),
  title: z.string(),
  actor: z.string(),
  policy_id: z.string().optional(),
  signal: z.string().optional(),
  // substring of `title` to emphasise (usually the resource name)
  highlight: z.string().optional(),
  branch: z.string().optional(),
  note: z.string().optional(),
  correlation_id: z.string(),
  occurred_at: z.string(),
  outcome: z.enum([
    "awaiting_approval",
    "applied",
    "verifying",
    "verified",
    "rolled_back",
    "blocked",
    "failed",
    "manual_intervention",
  ]),
});
export const auditEventListSchema = z.object({ items: z.array(auditEventSchema) });
export type AuditEvent = z.infer<typeof auditEventSchema>;

// -- Section 8.3 Recommendation detail ---------------------------------------
// Evidence quality is its own axis: a signal can be fresh-but-partial, or
// present-but-invalid. `value: null` means "no data" — never render it as 0.
export const evidenceItemSchema = z.object({
  signal: z.string(),
  value: z.number().nullable(),
  unit: z.string().optional(),
  aggregation: z.string().optional(),
  window: z.string(),
  source: z.string(),
  freshness: z.enum(["fresh", "stale", "unavailable"]),
  quality: z.enum(["valid", "partial", "missing", "invalid"]),
  reason_codes: z.array(z.string()),
});
export type EvidenceItem = z.infer<typeof evidenceItemSchema>;

export const recommendationDetailSchema = recommendationListItemSchema.extend({
  summary: z.string(),
  algorithm: z.object({ id: z.string(), version: z.string() }),
  current: z.record(z.string()),
  proposed: z.record(z.string()),
  confidence_assessment: z.object({
    observed_history: z.string(),
    required_history: z.string(),
    coverage_ratio: z.number(),
    reasons: z.array(z.string()),
  }),
  evidence: z.array(evidenceItemSchema),
  policy: z.object({
    decision: z.enum(["allowed", "requires_approval", "blocked"]),
    reasons: z.array(z.string()),
    required_approval: z.string().optional(),
    verification: z.array(z.string()),
  }),
  superseded_by: z.string().nullable(),
  // PRD §17 marks classification PLANNED: null means "backend doesn't supply it".
  workload_classification: z
    .object({ class: z.string(), classifier_version: z.string() })
    .nullable(),
  audit: z.array(auditEventSchema),
});
export type RecommendationDetail = z.infer<typeof recommendationDetailSchema>;

// -- Infrastructure scope options ------------------------------------------
export const scopeOptionSchema = z.object({ value: z.string(), label: z.string() });
export const scopeOptionsSchema = z.object({
  providers: z.array(scopeOptionSchema),
  sources: z.array(scopeOptionSchema),
  regions: z.array(scopeOptionSchema),
  resource_types: z.array(scopeOptionSchema),
});
export type ScopeOptions = z.infer<typeof scopeOptionsSchema>;