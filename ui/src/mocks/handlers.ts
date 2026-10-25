import { http, HttpResponse } from "msw";
import type { RecommendationListItem } from "@/schemas";
import {
  auditEvents,
  baseRecommendations,
  buildScopeOptions,
  buildSummary,
  detailFor,
} from "./fixtures";

export type Scenario =
  | "healthy"
  | "empty-cluster"
  | "no-recommendations"
  | "stale"
  | "partial-failure"
  | "backend-down";

let scenario: Scenario = "healthy";
// Demo only: pretend the evaluation finished four minutes before this mock session.
// Keep this timestamp across list refetches so the displayed age can increase.
// A fresh page load starts a new mock session; real data will use the backend's evaluation time.
const mockEvaluatedAt = new Date(Date.now() - 4 * 60_000).toISOString();
export function setScenario(next: Scenario) {
  scenario = next;
}

const unavailable = (title: string) =>
  HttpResponse.json(
    { title, status: 503, code: "backend_unavailable", retryable: true },
    { status: 503, headers: { "Content-Type": "application/problem+json" } },
  );

const isDown = () => scenario === "backend-down" || scenario === "partial-failure";
const isEmpty = () => scenario === "empty-cluster" || scenario === "no-recommendations";

const FILTERS: Record<string, (r: RecommendationListItem) => string | undefined> = {
  status: (r) => r.status,
  risk: (r) => r.risk,
  owner: (r) => r.owner,
  provider: (r) => r.resource.provider,
  environment: (r) => r.resource.environment,
  source: (r) => r.resource.source,
  region: (r) => r.resource.region,
  type: (r) => r.resource.type,
};

export const handlers = [
  http.get("*/api/v1/dashboard/summary", ({ request }) => {
    if (scenario === "backend-down") return unavailable("Service unavailable");
    const q = new URL(request.url).searchParams;
    return HttpResponse.json(
      buildSummary(
        {
          provider: q.get("provider") ?? undefined,
          source: q.get("source") ?? undefined,
          region: q.get("region") ?? undefined,
          type: q.get("type") ?? undefined,
        },
        q.get("window") ?? "P30D",
        {
          stale: scenario === "stale",
          degraded: scenario === "partial-failure",
          empty: scenario === "empty-cluster",
        },
      ),
    );
  }),

  http.get("*/api/v1/scope-options", () => {
    if (scenario === "backend-down") return unavailable("Scope options unavailable");
    return HttpResponse.json(
      scenario === "empty-cluster"
        ? { providers: [], sources: [], regions: [], resource_types: [] }
        : buildScopeOptions(),
    );
  }),

  http.get("*/api/v1/recommendations", ({ request }) => {
    if (isDown()) return unavailable("Recommendation service unavailable");

    const q = new URL(request.url).searchParams;
    let items = isEmpty() ? [] : [...baseRecommendations];

    // Unknown params are ignored here; the real API should return a
    // validation problem for them (design doc §6.7).
    for (const [key, read] of Object.entries(FILTERS)) {
      const wanted = q.get(key);
      if (wanted) items = items.filter((item) => read(item) === wanted);
    }
    // sort=priority: backend ranking order, superseded work excluded.
    if (q.get("sort") === "priority") {
      items = items.filter((item) => item.status !== "superseded");
    }
    return HttpResponse.json({ items, page: { next_cursor: null, has_more: false } });
  }),


  // http.get("*/api/v1/plugins", () => {
  //   if (scenario === "backend-down") {
  //     return HttpResponse.json(
  //       { title: "Capability service unavailable", status: 503, retryable: true },
  //       { status: 503, headers: { "Content-Type": "application/problem+json" } },
  //     );
  //   }
  //   const items = [
  //     {
  //       id: "kubernetes",
  //       display_name: "Kubernetes",
  //       capability: "Resource discovery",
  //       health: scenario === "partial-failure" ? "degraded" : "healthy",
  //       message: scenario === "partial-failure" ? "API server latency elevated" : undefined,
  //       last_checked_at: new Date().toISOString(),
  //     },
  //     {
  //       id: "prometheus",
  //       display_name: "Prometheus",
  //       capability: "Evidence collection",
  //       health: "healthy",
  //       last_checked_at: new Date().toISOString(),
  //     },
  //   ];
  //   return HttpResponse.json({ items });
  // }),

  // http.get("*/api/v1/audit-events", () => {
  //   if (scenario === "backend-down" || scenario === "partial-failure") {
  //     return HttpResponse.json(
  //       { title: "Audit service unavailable", status: 503, retryable: true },
  //       { status: 503, headers: { "Content-Type": "application/problem+json" } },
  //     );
  //   }

  //   const limit = Number(q.get("limit")) || undefined;
  //   const hasMore = limit !== undefined && items.length > limit;
  //   if (limit !== undefined) items = items.slice(0, limit);

  //   return HttpResponse.json({ items, page: { next_cursor: null, has_more: hasMore } });
  // }),

  http.get("*/api/v1/recommendations/:id", ({ params }) => {
    if (isDown()) return unavailable("Recommendation service unavailable");
    const detail = isEmpty() ? undefined : detailFor(String(params.id));
    if (!detail) {
      return HttpResponse.json(
        { title: "Recommendation not found", status: 404, code: "not_found" },
        { status: 404, headers: { "Content-Type": "application/problem+json" } },
      );
    }
    return HttpResponse.json(detail);
  }),

  // PRD §7.4: alpha normally shows Kubernetes + Prometheus only.
  // "partial-failure" demonstrates the Degraded state (§7.9).
  http.get("*/api/v1/plugins", () => {
    if (scenario === "backend-down") return unavailable("Capability service unavailable");
    const now = new Date().toISOString();
    return HttpResponse.json({
      items: [
        {
          id: "kubernetes",
          display_name: "Kubernetes",
          capability: "Resource discovery",
          health: scenario === "partial-failure" ? "degraded" : "healthy",
          message: scenario === "partial-failure" ? "API server latency elevated" : undefined,
          last_checked_at: now,
        },
        {
          id: "prometheus",
          display_name: "Prometheus",
          capability: "Evidence collection",
          health: "healthy",
          last_checked_at: now,
        },
      ],
    });
  }),

  // http.get("*/api/v1/audit-events", () => {
  //   if (isDown()) return unavailable("Audit service unavailable");
  //   return HttpResponse.json({ items: isEmpty() ? [] : auditEvents });
  // }),
  
];