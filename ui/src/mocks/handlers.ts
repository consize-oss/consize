import { http, HttpResponse } from "msw";
import type { RecommendationListItem, ScopeOptions } from "@/schemas";
import type { ResourceInventoryDetails } from "@/features/resources/resourceInventory";
import { baseResources } from "./resources";
import {
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

// Keep established choices while including resources that do not have recommendations.
function buildInventoryScopeOptions(): ScopeOptions {
  const existing = buildScopeOptions();
  const merge = (
    current: { value: string; label: string }[],
    values: string[],
    label: (value: string) => string = (value) => value,
  ) => {
    const options = new Map(current.map((option) => [option.value, option]));
    for (const value of values.filter(Boolean)) {
      if (!options.has(value)) options.set(value, { value, label: label(value) });
    }
    return [...options.values()].sort((a, b) => a.value.localeCompare(b.value));
  };
  return {
    providers: merge(existing.providers, baseResources.map((resource) => resource.provider),
      (value) => ({ aws: "AWS", kubernetes: "Kubernetes" }[value] ?? value)),
    sources: merge(existing.sources, baseResources.map((resource) => resource.source_plugin_id)),
    regions: merge(existing.regions, baseResources.map((resource) => resource.region)),
    resource_types: merge(existing.resource_types, baseResources.map((resource) =>
      (resource.metadata.demo_inventory as ResourceInventoryDetails).scope_type ?? resource.type)),
  };
}

let inventorySyncedAt: string | null = null;

function resourceInventoryForScenario() {
  return scenario === "empty-cluster" ? [] : baseResources.map((resource) => {
    const inventory = resource.metadata.demo_inventory as ResourceInventoryDetails;
    return {
      ...resource,
      ...(inventorySyncedAt ? {
        observed_at: inventorySyncedAt, last_seen_at: inventorySyncedAt, updated_at: inventorySyncedAt,
      } : {}),
      metadata: {
        ...resource.metadata,
        demo_inventory: {
          ...inventory,
          ...(scenario === "no-recommendations" ? { recommendation_ids: [] } : {}),
          ...(scenario === "stale" && inventory.telemetry?.status === "healthy" ? {
            telemetry: {
              ...inventory.telemetry, status: "stale",
              checked_at: new Date(Date.now() - 3 * 60 * 60_000).toISOString(),
            },
          } : {}),
        },
      },
    };
  });
}

export const handlers = [
  http.get("*/api/resources", () => {
    if (isDown()) return unavailable("Resource inventory unavailable");
    return HttpResponse.json({ resources: resourceInventoryForScenario() });
  }),

  http.post("*/api/discovery", () => {
    if (isDown()) return unavailable("Inventory sync unavailable");
    inventorySyncedAt = new Date().toISOString();
    const groups = new Map<string, typeof baseResources>();
    for (const resource of resourceInventoryForScenario()) {
      const resources = groups.get(resource.source_plugin_id) ?? [];
      resources.push(resource);
      groups.set(resource.source_plugin_id, resources);
    }
    return HttpResponse.json({
      providers: [...groups.entries()].map(([plugin_id, resources]) => ({
        plugin_id, count: resources.length, resources,
      })),
    });
  }),

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
        : buildInventoryScopeOptions(),
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
    // Overview requests only five rows. Without a limit, Recommendations still
    // receives the full filtered list for its local pagination and status counts.
    const requestedLimit = Number(q.get("limit"));
    const limit = Number.isSafeInteger(requestedLimit) && requestedLimit > 0 ? requestedLimit : undefined;
    const requestedOffset = Number(q.get("cursor"));
    const offset = Number.isSafeInteger(requestedOffset) && requestedOffset >= 0 ? requestedOffset : 0;
    const end = limit === undefined ? items.length : Math.min(offset + limit, items.length);
    const hasMore = end < items.length;
    items = items.slice(offset, end);
    return HttpResponse.json({
      items,
      page: { next_cursor: hasMore ? String(end) : null, has_more: hasMore },
    });
  }),


  http.get("*/api/v1/plugins", () => {
    if (scenario === "backend-down") {
      return HttpResponse.json(
        { title: "Capability service unavailable", status: 503, retryable: true },
        { status: 503, headers: { "Content-Type": "application/problem+json" } },
      );
    }
    const items = [
      {
        id: "kubernetes",
        display_name: "Kubernetes",
        capability: "Resource discovery",
        health: scenario === "partial-failure" ? "degraded" : "healthy",
        message: scenario === "partial-failure" ? "API server latency elevated" : undefined,
        last_checked_at: new Date().toISOString(),
      },
      {
        id: "prometheus",
        display_name: "Prometheus",
        capability: "Evidence collection",
        health: "healthy",
        last_checked_at: new Date().toISOString(),
      },
    ];
    return HttpResponse.json({ items });
  }),

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