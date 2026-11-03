import { useQuery } from "@tanstack/react-query";
import { ApiError, apiGet } from "@/lib/api-client";
import { pluginDetailSchema, pluginListSchema } from "@/schemas";

export type PluginFilters = { category?: string; q?: string };

export const pluginKeys = {
  list: (f: PluginFilters) => ["plugins", "list", f] as const,
  detail: (id: string) => ["plugins", "detail", id] as const,
};

function toQuery(f: PluginFilters) {
  const q = new URLSearchParams(
    Object.entries(f).filter((e): e is [string, string] => Boolean(e[1])),
  ).toString();
  return q ? `?${q}` : "";
}

export function usePlugins(filters: PluginFilters = {}) {
  return useQuery({
    queryKey: pluginKeys.list(filters),
    queryFn: ({ signal }) => apiGet(`/api/v1/plugins${toQuery(filters)}`, pluginListSchema, signal),
    // Keep the last list on screen while a filter refetches so tab counts
    // and cards don't flash to skeletons on every keystroke.
    placeholderData: (prev) => prev,
    refetchInterval: 30_000,
  });
}

// No placeholderData: a plugin's panel must never show another plugin's data.
export function usePluginDetail(id: string | undefined) {
  return useQuery({
    queryKey: pluginKeys.detail(id ?? ""),
    queryFn: ({ signal }) =>
      apiGet(`/api/v1/plugins/${encodeURIComponent(id!)}`, pluginDetailSchema, signal),
    enabled: Boolean(id),
    retry: (count, err) => !(err instanceof ApiError && err.status === 404) && count < 1,
    refetchInterval: 30_000,
  });
}