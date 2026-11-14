import { http, HttpResponse } from "msw";
import type { components } from "../../lib/generated/api";
import { baseRecommendations, detailFor } from "./fixtures";
import type { Scenario } from "./handlers";

type Resource = components["schemas"]["Resource"];
type Recommendation = components["schemas"]["Recommendation"];
type Job = Pick<
  components["schemas"]["Job"],
  | "id"
  | "action_id"
  | "recommendation_id"
  | "state"
  | "window_start"
  | "deadline"
  | "updated_at"
> & { verification_result?: { status: string; reasons: string[] } };
const responseError = (status: number, error: string) =>
  HttpResponse.json({ error }, { status });

// Loaded only by explicit development mocks or tests. No provider is contacted.
export function createRuntimeHandlers(
  getScenario: () => Scenario,
  readInventory: () => Resource[],
) {
  let recommendations: Recommendation[] | undefined;
  let jobs: Job[] = [];
  let events: components["schemas"]["ActionEvent"][] = [];
  const reset = () => {
    recommendations = undefined;
    jobs = [];
    events = [];
  };
  const unavailable = () => responseError(503, "Simulated backend outage");
  const resources = () =>
    readInventory().map((resource) => {
      const inventory = resource.metadata.inventory as
        | { recommendation_ids?: string[] }
        | undefined;
      return {
        ...resource,
        metadata: {
          ...resource.metadata,
          ...(inventory
            ? {
                inventory: {
                  ...inventory,
                  recommendation_ids: inventory.recommendation_ids?.map((id) =>
                    String(
                      baseRecommendations.findIndex((rec) => rec.id === id) + 1,
                    ),
                  ),
                },
              }
            : {}),
        },
      };
    });
  const timestamp = () => new Date().toISOString();
  const rawRecommendations = () => {
    if (
      getScenario() === "empty-cluster" ||
      getScenario() === "no-recommendations"
    )
      return recommendations ?? [];
    return (recommendations ??= baseRecommendations.flatMap(
      (rec, index): Recommendation[] => {
        if (!resources().some((resource) => resource.id === rec.resource.id))
          return [];
        const detail = detailFor(rec.id);
        const now = timestamp();
        const stale = getScenario() === "stale";
        const start = new Date(Date.now() - 30 * 60_000).toISOString();
        const snapshot: components["schemas"]["EvidenceRecord"][] = (
          detail?.evidence ?? []
        ).map((e, i) => ({
          schema_version: 1,
          id: `mock-evidence-${index + 1}-${i}`,
          resource_id: rec.resource.id,
          plugin_id: "prometheus-metrics",
          signal: e.signal,
          query: "Simulated development measurement",
          kind: "aggregated",
          aggregation: e.aggregation ?? "p95",
          unit: e.unit ?? "count",
          original_unit: e.unit ?? "count",
          scale: 1,
          value: e.value,
          sample_count: 60,
          window_start: start,
          window_end: now,
          collected_at: now,
          fresh_until: new Date(
            Date.now() + (stale ? -60_000 : 60_000),
          ).toISOString(),
          quality: stale ? "stale" : e.quality,
        }));
        return [
          {
            id: index + 1,
            resource_id: rec.resource.id,
            plugin_id: "mock-action",
            algorithm_id: detail?.algorithm.id ?? "mock-headroom",
            algorithm_version: "1",
            action_type: "mock.resize",
            title: rec.title,
            summary: detail?.summary ?? rec.target_summary,
            current: detail?.current ?? {},
            proposed: detail?.proposed ?? {},
            parameters: {
              confidence_assessment: detail?.confidence_assessment ?? {
                observed_history: "30m",
                required_history: "30m",
                coverage_ratio: 1,
                reasons: ["Simulated development evidence"],
              },
            },
            estimated_savings_monthly: rec.estimated_savings.amount_minor / 100,
            confidence: rec.confidence,
            risk: rec.risk,
            evidence: ["Development simulation only"],
            evidence_snapshot: snapshot,
            policy_id: "mock-policy",
            status:
              rec.status === "superseded"
                ? "superseded"
                : rec.status === "blocked"
                  ? "rejected"
                  : "pending",
            created_at: now,
            updated_at: now,
          },
        ];
      },
    ));
  };
  const find = (id: string | readonly string[] | undefined) =>
    rawRecommendations().find((rec) => String(rec.id) === id);
  const plan = (rec: Recommendation) => ({
    summary: `Simulated change: ${rec.title}`,
    plugin_id: rec.plugin_id,
    resource_id: rec.resource_id,
    action_type: rec.action_type,
    original_state: rec.current,
    applied_state: rec.proposed,
    diff: { current: rec.current, proposed: rec.proposed },
    rollback_available: true,
    requires_approval: true,
    preflight: [
      {
        id: "development-simulation",
        status:
          getScenario() === "stale" ||
          rec.status === "rejected" ||
          rec.status === "superseded"
            ? "blocked"
            : "passed",
        message:
          getScenario() === "stale"
            ? "Simulated stale evidence blocks execution"
            : "Development simulation; no infrastructure will change",
      },
    ],
  });
  const record = (rec: Recommendation, job: Job) =>
    events.push({
      id: events.length + 1,
      action_id: job.action_id,
      recommendation_id: rec.id,
      resource_id: rec.resource_id,
      plugin_id: rec.plugin_id,
      action_type: rec.action_type,
      actor: "mock-operator",
      mode: "approved",
      result: job.state,
      message: `Simulated workflow: ${job.state}`,
      parameters: {},
      policy_decision: {
        policy_id: "mock-policy",
        decision: "approved",
        reasons: ["Simulated explicit approval"],
      },
      created_at: timestamp(),
    });
  const simulate = (rec: Recommendation, job: Job) => {
    // Deterministic read-driven simulation, not a real verification worker.
    if (job.state === "prepared") {
      job.state = "verifying";
      job.updated_at = timestamp();
      record(rec, job);
    } else if (
      job.state === "verifying" ||
      job.state === "rollback_verifying"
    ) {
      job.state = job.state === "verifying" ? "verified" : "rolled_back";
      job.verification_result = {
        status: "passed",
        reasons: ["Simulated development result, not live qualification"],
      };
      job.updated_at = timestamp();
      record(rec, job);
    }
  };
  const plugins = () => [
    {
      manifest: {
        id: "prometheus-metrics",
        display_name: "Prometheus Metrics (simulated)",
        version: "0.3.0-dev",
        category: "metrics",
        supported_resource_types: [
          ...new Set(resources().map((resource) => resource.type)),
        ],
        supported_action_types: [],
        capabilities: ["metrics.read"],
        can_mutate_infrastructure: false,
        requires_approval: false,
      },
      health: {
        status: getScenario() === "partial-failure" ? "degraded" : "healthy",
        message:
          getScenario() === "stale"
            ? "Simulated stale telemetry; query API reachable"
            : "Simulated metrics connection",
        checked_at: timestamp(),
      },
    },
    {
      manifest: {
        id: "mock-action",
        display_name: "Development action simulator",
        version: "0.3.0-dev",
        category: "action",
        supported_resource_types: [
          ...new Set(resources().map((resource) => resource.type)),
        ],
        supported_action_types: ["mock.resize"],
        capabilities: ["action.plan", "action.execute", "action.preflight"],
        can_mutate_infrastructure: true,
        requires_approval: true,
      },
      health: {
        status: "healthy",
        message: "In-memory simulation only; never contacts a provider",
        checked_at: timestamp(),
      },
    },
  ];
  return {
    reset,
    handlers: [
      http.get("*/api/resources", () =>
        ["backend-down", "partial-failure"].includes(getScenario())
          ? unavailable()
          : HttpResponse.json({ resources: resources() }),
      ),
      http.get("*/api/dashboard", () =>
        getScenario() === "backend-down"
          ? unavailable()
          : HttpResponse.json({
              resources: resources(),
              recommendations: rawRecommendations(),
              jobs,
              actions: events,
              action_records: [],
              plugins: plugins(),
              stats: {
                projected_savings_monthly: 0,
                verified_savings_monthly: 0,
                pending_approvals: rawRecommendations().filter(
                  (rec) => rec.status === "pending",
                ).length,
                rollback_rate: 0,
                open_recommendations: rawRecommendations().length,
                executed_actions: jobs.length,
              },
              verification: {
                enabled: true,
                metrics_plugin_id: "prometheus-metrics",
                checks: [],
                rollback_on_failure: true,
                rollback_on_timeout: true,
                timeout: "10m",
                isolation: "30m",
                wait: "5m",
                max_restart_increase: 0,
                max_memory_p95_increase_ratio: 1.25,
                max_cpu_p95_increase_ratio: 1.5,
              },
              policy: {
                id: "mock-policy",
                name: "Development simulation policy",
                mode: "Explicit approval required (simulated)",
                approval_model: "Review and confirm; simulation only",
                verification: "Simulated verification, not live evidence",
              },
            }),
      ),
      http.get("*/api/health", () =>
        getScenario() === "backend-down"
          ? unavailable()
          : HttpResponse.json({
              status:
                getScenario() === "partial-failure" ? "degraded" : "healthy",
              store: "healthy",
              durable_worker: false,
              plugins: plugins(),
            }),
      ),
      http.get("*/api/plugins", () =>
        getScenario() === "backend-down"
          ? unavailable()
          : HttpResponse.json({ plugins: plugins() }),
      ),
      http.get("*/api/jobs", () => {
        if (getScenario() === "backend-down") return unavailable();
        for (const job of jobs) {
          const rec = find(String(job.recommendation_id));
          if (rec) simulate(rec, job);
        }
        return HttpResponse.json({ jobs });
      }),
      http.get("*/api/actions", () =>
        getScenario() === "backend-down" || getScenario() === "partial-failure"
          ? unavailable()
          : HttpResponse.json({ actions: events }),
      ),
      http.post("*/api/recommendations/generate", ({ request }) => {
        if (["backend-down", "partial-failure"].includes(getScenario()))
          return unavailable();
        if (getScenario() === "stale")
          return responseError(
            400,
            "Simulated stale evidence: fresh measurements are required",
          );
        const resource = resources().find(
          (resource) =>
            resource.id ===
            new URL(request.url).searchParams.get("resource_id"),
        );
        if (!resource)
          return responseError(404, "Resource not found in this scenario");
        const rec: Recommendation = {
          id: Math.max(0, ...rawRecommendations().map((rec) => rec.id)) + 1,
          resource_id: resource.id,
          plugin_id: "mock-action",
          algorithm_id: "mock-headroom",
          algorithm_version: "1",
          action_type: "mock.resize",
          title: `Review ${resource.name}`,
          summary: "Simulated request reduction; no real change",
          current: { request: 512 },
          proposed: { request: 384 },
          parameters: {
            confidence_assessment: {
              observed_history: "30m",
              required_history: "30m",
              coverage_ratio: 1,
              reasons: ["Simulated development history"],
            },
          },
          estimated_savings_monthly: 0,
          confidence: "medium",
          risk: "low",
          evidence: ["Simulated evidence"],
          policy_id: "mock-policy",
          status: "pending",
          created_at: timestamp(),
          updated_at: timestamp(),
        };
        recommendations = [...rawRecommendations(), rec];
        return HttpResponse.json({ recommendations: [rec] });
      }),
      http.post("*/api/recommendations/:id/plan", ({ params }) => {
        if (["backend-down", "partial-failure"].includes(getScenario()))
          return unavailable();
        const rec = find(params.id);
        if (!rec) return responseError(404, "Recommendation not found");
        const prepared = plan(rec);
        return HttpResponse.json({
          planned: {
            plan: prepared,
            policy_decision: {
              policy_id: "mock-policy",
              decision: prepared.preflight.every(
                (check) => check.status === "passed",
              )
                ? "recommend_only"
                : "blocked",
              reasons: ["Development simulation only"],
            },
          },
        });
      }),
      ...(["execute", "recover"] as const).map((operation) =>
        http.post(
          `*/api/recommendations/:id/${operation}`,
          async ({ params, request }) => {
            if (["backend-down", "partial-failure"].includes(getScenario()))
              return unavailable();
            const body = await request.json().catch(() => null);
            if (
              !body ||
              typeof body !== "object" ||
              !("mode" in body) ||
              body.mode !== "approved"
            )
              return responseError(400, "Explicit approval required");
            const rec = find(params.id);
            if (!rec) return responseError(404, "Recommendation not found");
            const existing = jobs.find(
              (job) => job.recommendation_id === rec.id,
            );
            if (operation === "execute" && existing)
              return HttpResponse.json({ job: existing }, { status: 202 });
            if (
              operation === "recover" &&
              existing?.state !== "manual_intervention"
            )
              return responseError(
                400,
                "Only unresolved actions can be recovered",
              );
            if (
              !plan(rec).preflight.every((check) => check.status === "passed")
            )
              return responseError(
                400,
                "Simulated preflight blocked execution",
              );
            const job: Job = existing ?? {
              id: rec.id,
              action_id: rec.id,
              recommendation_id: rec.id,
              state: "prepared",
              window_start: timestamp(),
              deadline: new Date(Date.now() + 60_000).toISOString(),
              updated_at: timestamp(),
            };
            if (operation === "recover") job.state = "rollback_verifying";
            if (!existing) jobs.push(job);
            record(rec, job);
            return HttpResponse.json({ job }, { status: 202 });
          },
        ),
      ),
    ],
  };
}
