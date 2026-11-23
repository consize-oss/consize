"use client";

import { useSyncExternalStore } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import type { components, operations } from "../../../lib/generated/api";
import { ApiError, apiGet, apiPost } from "@/lib/api-client";
import { readDemoResourceInventory, type ResourceInventoryItem, type ResourceScope } from "./resourceInventoryModel";

export type Resource = components["schemas"]["Resource"];
export const RESOURCE_QUERY_KEY = ["resources", "inventory"] as const;

// Validate the existing registry response; generated API declarations remain unchanged.
const resourceSchema: z.ZodType<Resource> = z.object({
  schema_version: z.number().int(), id: z.string(), type: z.string(), provider: z.string(),
  provider_resource_id: z.string(), name: z.string(), environment: z.string(), owner: z.string(),
  region: z.string(), account: z.string(), criticality: z.enum(["low", "medium", "high"]),
  lifecycle_state: z.enum(["active", "stale", "deleted"]),
  support_status: z.enum(["fully_supported", "model_only", "unsupported"]),
  labels: z.record(z.string(), z.string()), metadata: z.record(z.string(), z.unknown()), current_state: z.record(z.string(), z.unknown()),
  monthly_cost: z.number(), source_plugin_id: z.string(),
  observed_at: z.string().datetime(), first_seen_at: z.string().datetime(), last_seen_at: z.string().datetime(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(),
});
export const resourceListSchema = z.object({ resources: z.array(resourceSchema) });
const assessedInventoryRowSchema = z.object({
  resource: resourceSchema,
  assessment: z.object({
    version: z.literal("inventory-v1"),
    assessed_at: z.string().datetime(),
    owner: z.string().nullable(),
    telemetry: z.object({
      status: z.enum(["usable", "stale", "unmonitored", "unknown"]),
      source_plugin_ids: z.array(z.string()),
      last_measurement_at: z.string().datetime().nullable(),
      quality: z.object({
        thresholds: z.object({ max_age: z.string() }),
        dimensions: z.array(z.object({ signal: z.string() })),
      }).optional(),
    }),
    policy: z.object({ status: z.enum(["covered", "uncovered", "unknown"]), policy_ids: z.array(z.string()) }),
    actions: z.object({ status: z.enum(["supported", "unsupported", "unknown"]) }),
    optimization_status: z.enum(["awaiting_evidence", "no_active_recommendations", "recommendations_available", "executing"]),
    active_recommendation_ids: z.array(z.number().int()),
  }),
});
const assessedInventoryPageSchema = z.object({
  resources: z.array(assessedInventoryRowSchema).max(200),
  filtered_total: z.number().int().nonnegative(),
  snapshot_id: z.string().min(1),
  next_cursor: z.string(),
  has_more: z.boolean(),
});

function toInventoryItem({ resource, assessment }: z.infer<typeof assessedInventoryRowSchema>): ResourceInventoryItem {
  const { telemetry, policy, actions } = assessment;
  const signals = telemetry.quality?.dimensions.map(({ signal }) => signal).filter(Boolean);
  return {
    ...resource,
    owner: assessment.owner ?? "",
    inventory: {
      assessed_at: assessment.assessed_at,
      telemetry: {
        status: telemetry.status === "usable" ? "healthy" : telemetry.status,
        source: telemetry.source_plugin_ids.join(", ") || undefined,
        checked_at: telemetry.last_measurement_at ?? undefined,
        freshness_limit: telemetry.quality?.thresholds.max_age,
        signals: signals ? [...new Set(signals)] : undefined,
      },
      policy_coverage: policy.status,
      policy_ids: policy.policy_ids,
      action_path_available: actions.status === "unknown" ? undefined : actions.status === "supported",
      optimization_status: actions.status === "unsupported" ? "action_unsupported"
        : assessment.optimization_status === "no_active_recommendations" ? "no_action_needed"
          : assessment.optimization_status === "awaiting_evidence" ? "awaiting_evidence" : "recommendations",
      recommendation_ids: assessment.active_recommendation_ids.map(String),
    },
  };
}

export async function fetchAssessedInventory(signal?: AbortSignal, scope: ResourceScope = {}): Promise<ResourceInventoryItem[]> {
  const params = new URLSearchParams({ limit: "200" });
  if (scope.provider) params.set("provider", scope.provider);
  if (scope.source) params.set("source_plugin_id", scope.source);
  if (scope.region) params.set("region", scope.region);
  if (scope.type) params.set("type", scope.type);
  const items: ResourceInventoryItem[] = [];
  const seenCursors = new Set<string>();
  let snapshot: string | undefined;
  let expectedTotal: number | undefined;
  // The backend's Attention total includes unknown assessments. Read the scoped
  // snapshot before deriving the confirmed-only views and counts in the UI.
  for (;;) {
    const page = await apiGet(`/api/resources/inventory?${params}`, assessedInventoryPageSchema, signal);
    snapshot ??= page.snapshot_id;
    expectedTotal ??= page.filtered_total;
    if (snapshot !== page.snapshot_id || expectedTotal !== page.filtered_total) throw new ApiError(409);
    items.push(...page.resources.map(toInventoryItem));
    if (items.length > expectedTotal || (!page.has_more && items.length !== expectedTotal)) throw new ApiError(-1);
    if (!page.has_more) return items;
    if (!page.resources.length || !page.next_cursor || seenCursors.has(page.next_cursor)) throw new ApiError(-1);
    seenCursors.add(page.next_cursor);
    params.set("cursor", page.next_cursor);
  }
}

type DiscoveryResponse = operations["runDiscovery"]["responses"][200]["content"]["application/json"];
export const resourceDiscoverySchema: z.ZodType<DiscoveryResponse> = z.object({
  providers: z.array(z.object({
    plugin_id: z.string(), count: z.number().int().nonnegative(), resources: z.array(resourceSchema),
  })),
});

const subscribe = () => () => {};
const getClientSnapshot = () => true;
const getServerSnapshot = () => false;

export function useResourceList(scope: ResourceScope) {
  const hydrated = useSyncExternalStore(subscribe, getClientSnapshot, getServerSnapshot);
  const query = useQuery({
    queryKey: [...RESOURCE_QUERY_KEY, scope],
    queryFn: async ({ signal }) => {
      if (process.env.NODE_ENV === "development" && process.env.NEXT_PUBLIC_USE_MOCKS === "true") {
        const demo = await apiGet("/api/resources", resourceListSchema, signal);
        return demo.resources.map(readDemoResourceInventory);
      }
      return fetchAssessedInventory(signal, scope);
    },
    enabled: hydrated,
    retry: (count, error) =>
      !(error instanceof ApiError && [401, 403, -1].includes(error.status)) && count < 1,
    refetchInterval: 30_000,
  });
  // Withhold cached data during hydration so dropdown options match the server too.
  return { ...query, data: hydrated ? query.data : undefined, isPending: !hydrated || query.isPending };
}

export function useResourceSync() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationKey: ["resources", "sync"],
    mutationFn: () => apiPost("/api/discovery", resourceDiscoverySchema),
    retry: false,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: RESOURCE_QUERY_KEY });
    },
  });
}
