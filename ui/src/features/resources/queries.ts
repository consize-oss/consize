"use client";

import { useSyncExternalStore } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import type { components, operations } from "../../../lib/generated/api";
import { ApiError, apiGet, apiPost } from "@/lib/api-client";

export type Resource = components["schemas"]["Resource"];
export const RESOURCE_QUERY_KEY = ["resources", "inventory"] as const;

// Validate the existing registry response; generated API declarations remain unchanged.
const resourceSchema: z.ZodType<Resource> = z.object({
  schema_version: z.number().int(), id: z.string(), type: z.string(), provider: z.string(),
  provider_resource_id: z.string(), name: z.string(), environment: z.string(), owner: z.string(),
  region: z.string(), account: z.string(), criticality: z.enum(["low", "medium", "high"]),
  lifecycle_state: z.enum(["active", "stale", "deleted"]),
  support_status: z.enum(["fully_supported", "model_only", "unsupported"]),
  labels: z.record(z.string()), metadata: z.record(z.unknown()), current_state: z.record(z.unknown()),
  monthly_cost: z.number(), source_plugin_id: z.string(),
  observed_at: z.string().datetime(), first_seen_at: z.string().datetime(), last_seen_at: z.string().datetime(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(),
});
export const resourceListSchema = z.object({ resources: z.array(resourceSchema) });
type DiscoveryResponse = operations["runDiscovery"]["responses"][200]["content"]["application/json"];
export const resourceDiscoverySchema: z.ZodType<DiscoveryResponse> = z.object({
  providers: z.array(z.object({
    plugin_id: z.string(), count: z.number().int().nonnegative(), resources: z.array(resourceSchema),
  })),
});

const subscribe = () => () => {};
const getClientSnapshot = () => true;
const getServerSnapshot = () => false;

export function useResourceList() {
  const hydrated = useSyncExternalStore(subscribe, getClientSnapshot, getServerSnapshot);
  const query = useQuery({
    queryKey: RESOURCE_QUERY_KEY,
    queryFn: ({ signal }) => apiGet("/api/resources", resourceListSchema, signal),
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
