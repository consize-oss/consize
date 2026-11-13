import { it } from "node:test";
import assert from "node:assert/strict";
import {
  canApprovePlan,
  runtimeDashboardSchema,
  recommendationViews,
  pluginsView,
  summaryView,
  unavailableMoney,
} from "./runtime-api";
import { formatMoney } from "./format";
const empty = () =>
  runtimeDashboardSchema.parse({
    resources: [],
    recommendations: [],
    jobs: [],
    plugins: [],
    verification: { enabled: false, metrics_plugin_id: "" },
    policy: {
      id: "oss-foundation-v1",
      name: "OSS",
      mode: "Production approval required",
      verification: "When configured",
      approval_model: "Review then approve",
    },
  });
it("normalizes Go nil collections without creating operational rows", () => {
  const data = runtimeDashboardSchema.parse({
    ...empty(),
    resources: null,
    recommendations: null,
    jobs: null,
    plugins: null,
  });
  assert.deepEqual(recommendationViews(data).items, []);
  assert.equal(pluginsView(data).summary.counts.installed, 0);
  assert.equal(summaryView(data, {}).pending_approvals_mine, null);
});
it("missing pricing is unavailable, never a measured zero-dollar saving", () => {
  assert.deepEqual(
    summaryView(empty(), {}).projected_savings,
    unavailableMoney,
  );
  assert.equal(formatMoney(unavailableMoney).display, "—");
});
it("runtime model rejects schema drift instead of rendering fallback rows", () => {
  assert.equal(
    runtimeDashboardSchema.safeParse({
      ...empty(),
      resources: [{ id: "broken" }],
    }).success,
    false,
  );
  assert.equal(
    runtimeDashboardSchema.safeParse({
      ...empty(),
      jobs: [{ id: 1, state: "verified" }],
    }).success,
    false,
  );
});
it("plugin connectivity is not telemetry freshness or package signature verification", () => {
  const data = empty();
  data.plugins.push({
    manifest: {
      id: "test",
      display_name: "Test",
      version: "1",
      category: "metrics",
      supported_resource_types: [],
      supported_action_types: [],
      capabilities: ["metrics.read"],
      can_mutate_infrastructure: false,
      requires_approval: false,
    },
    health: {
      status: "healthy",
      message: "API reachable",
      checked_at: new Date().toISOString(),
    },
  });
  const view = pluginsView(data);
  assert.equal(view.items[0].health, "healthy");
  assert.equal(view.summary.ingestion.fresh_ratio, null);
  assert.equal(view.summary.trust.assessed, false);
  assert.equal(view.items[0].source, "unknown");
});

it("approval is disabled for blocked, absent or incomplete preflight", () => {
  const plan = {
    plan: {
      summary: "Resize",
      requires_approval: true,
      preflight: [{ id: "qos", status: "passed", message: "Pass" }],
    },
    policy_decision: { decision: "recommend_only", reasons: [] },
  };
  assert.equal(canApprovePlan(plan), true);
  assert.equal(canApprovePlan(undefined), false);
  assert.equal(
    canApprovePlan({
      ...plan,
      policy_decision: { decision: "blocked", reasons: [] },
    }),
    false,
  );
  assert.equal(
    canApprovePlan({ ...plan, plan: { ...plan.plan, preflight: [] } }),
    false,
  );
  assert.equal(
    canApprovePlan({
      ...plan,
      plan: {
        ...plan.plan,
        preflight: [{ id: "qos", status: "unknown", message: "No evidence" }],
      },
    }),
    false,
  );
});

function withRecommendation() {
  const now = new Date().toISOString();
  return runtimeDashboardSchema.parse({
    ...empty(),
    resources: [
      {
        schema_version: 1,
        id: "test-resource",
        type: "kubernetes.deployment",
        provider: "kubernetes",
        provider_resource_id: "dev/test",
        name: "checkout",
        environment: "development",
        owner: "platform",
        region: "local",
        account: "desktop",
        criticality: "low",
        lifecycle_state: "active",
        support_status: "fully_supported",
        labels: { team: "platform" },
        metadata: { namespace: "dev" },
        current_state: { memory: 512 },
        monthly_cost: 0,
        source_plugin_id: "discovery",
        observed_at: now,
        first_seen_at: now,
        last_seen_at: now,
        created_at: now,
        updated_at: now,
      },
    ],
    recommendations: [
      {
        id: 7,
        resource_id: "test-resource",
        title: "Reduce request",
        summary: "512 to 384",
        algorithm_id: "headroom",
        current: { request: 512 },
        proposed: { request: 384 },
        parameters: {},
        confidence: "medium",
        risk: "low",
        status: "pending",
        policy_id: "policy",
        created_at: now,
        updated_at: now,
      },
    ],
  });
}
it("joins real recommendation identities to inventory and filters the complete dataset", () => {
  const data = withRecommendation();
  assert.equal(
    recommendationViews(data, { provider: "kubernetes", q: "platform" })
      .items[0].resource.id,
    "test-resource",
  );
  assert.equal(recommendationViews(data, { provider: "aws" }).items.length, 0);
  assert.equal(
    recommendationViews(data, { status: "pending_approval" }).items.length,
    1,
  );
  data.recommendations[0].resource_id = "missing";
  assert.throws(() => recommendationViews(data), { status: -1 });
});
it("failed and unresolved jobs never appear as successful execution", () => {
  const data = withRecommendation();
  const now = new Date().toISOString();
  for (const state of ["failed", "cancelled", "manual_intervention"]) {
    data.jobs = [
      {
        id: 7,
        recommendation_id: 7,
        state,
        window_start: now,
        deadline: now,
        updated_at: now,
      },
    ];
    assert.equal(recommendationViews(data).items[0].status, "blocked");
    assert.equal(recommendationViews(data).items[0].workflow_state, state);
  }
});
