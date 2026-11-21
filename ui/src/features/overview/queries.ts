"use client";
import { useQuery } from "@tanstack/react-query";
import { apiGet, ApiError } from "@/lib/api-client";
import { dashboardSummarySchema } from "@/schemas";
import {
  actionEventsSchema,
  auditView,
  detailView,
  readDashboard,
  recommendationViews,
  summaryView,
} from "@/lib/runtime-api";
export type SummaryParams = {
  provider?: string;
  source?: string;
  region?: string;
  type?: string;
  window?: string;
};
export const queryKeys = {
  dashboard: {
    summary: (params: SummaryParams = {}) =>
      ["dashboard", "summary", params] as const,
  },
  recommendations: {
    all: ["recommendations"] as const,
    list: (filters: Record<string, string | undefined>) =>
      ["recommendations", "list", filters] as const,
    detail: (id: string) => ["recommendations", "detail", id] as const,
  },
  scopeOptions: ["scope-options"] as const,
  auditEvents: ["audit-events"] as const,
};
const retry = (count: number, error: unknown) =>
  !(error instanceof ApiError && [401, 403, 404, -1].includes(error.status)) &&
  count < 1;
export function useDashboardSummary(params: SummaryParams = {}) {
  const simulated = process.env.NODE_ENV === "development" && process.env.NEXT_PUBLIC_USE_MOCKS === "true";
  const realSummary = useQuery({
    queryKey: ["runtime-dashboard"],
    queryFn: ({ signal }) => readDashboard(signal),
    select: (data) => summaryView(data, params),
    enabled: !simulated,
    refetchInterval: 30_000,
    retry,
  });
  const query = new URLSearchParams(Object.entries(params).filter((entry): entry is [string, string] => entry[1] !== undefined)).toString();
  const sampleSummary = useQuery({
    queryKey: ["simulation-summary", params],
    queryFn: ({ signal }) => apiGet(`/api/v1/dashboard/summary${query ? `?${query}` : ""}`, dashboardSummarySchema, signal),
    enabled: simulated,
    refetchInterval: 30_000,
    retry,
  });
  return simulated ? sampleSummary : realSummary;
}
export function useRecommendationList(
  filters: Record<string, string | undefined> = {},
) {
  return useQuery({
    queryKey: ["runtime-dashboard"],
    queryFn: ({ signal }) => readDashboard(signal),
    select: (data) => recommendationViews(data, filters),
    refetchInterval: 30_000,
    retry,
  });
}
export function useRecommendationDetail(id: string) {
  return useQuery({
    queryKey: queryKeys.recommendations.detail(id),
    queryFn: ({ signal }) => detailView(id, signal),
    retry,
  });
}
export function useScopeOptions() {
  return useQuery({
    queryKey: ["runtime-dashboard"],
    queryFn: ({ signal }) => readDashboard(signal),
    select: (data) => {
      const options = (
        key: "provider" | "source_plugin_id" | "region" | "type",
      ) =>
        Array.from(new Set(data.resources.map((x) => x[key])))
          .sort()
          .map((value) => ({ value, label: value }));
      return {
        providers: options("provider"),
        sources: options("source_plugin_id"),
        regions: options("region"),
        resource_types: options("type"),
      };
    },
    staleTime: 60_000,
    retry,
  });
}
export function useAuditEvents() {
  return useQuery({
    queryKey: queryKeys.auditEvents,
    queryFn: async ({ signal }) => ({
      items: (await apiGet("/api/actions", actionEventsSchema, signal)).actions
        .map(auditView)
        .reverse(),
    }),
    refetchInterval: 30_000,
    retry,
  });
}
