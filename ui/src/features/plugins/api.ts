"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { z } from "zod";
import { ApiError, apiGet } from "@/lib/api-client";
import { problemSchema } from "@/schemas";
import {
  configurationSchema,
  pluginDetailSchema,
  pluginListSchema,
  type ConfigurationInput,
} from "./schemas";

// TODO: move into lib/api-client next to apiGet/apiPost. It lives here because
// the shared apiPost doesn't take a body and this feature needs POST and PUT
// with one.
const API_BASE = process.env.NEXT_PUBLIC_CONSIZE_API_BASE_URL ?? "http://127.0.0.1:8080";

async function sendJson<T>(method: "POST" | "PUT", path: string, body: unknown, schema: z.ZodType<T>): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      cache: "no-store",
    });
  } catch {
    throw new ApiError(0);
  }
  const json: unknown = await res.json().catch(() => undefined);
  if (!res.ok) {
    const problem = problemSchema.safeParse(json);
    throw new ApiError(res.status, problem.success ? problem.data : undefined);
  }
  const parsed = schema.safeParse(json);
  if (!parsed.success) throw new ApiError(-1);
  return parsed.data;
}

export const pluginKeys = {
  all: ["plugins"] as const,
  list: ["plugins", "list"] as const,
  detail: (id: string) => ["plugins", "detail", id] as const,
  configuration: (id: string) => ["plugins", "configuration", id] as const,
};

export function usePluginCatalog() {
  return useQuery({
    queryKey: pluginKeys.list,
    queryFn: ({ signal }) => apiGet("/api/v1/plugins", pluginListSchema, signal),
    refetchInterval: 30_000,
  });
}

// No placeholderData: a plugin's page must never show another plugin's data.
export function usePlugin(id: string) {
  return useQuery({
    queryKey: pluginKeys.detail(id),
    queryFn: ({ signal }) => apiGet(`/api/v1/plugins/${encodeURIComponent(id)}`, pluginDetailSchema, signal),
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    refetchInterval: 30_000,
  });
}

export function usePluginConfiguration(id: string, enabled: boolean) {
  return useQuery({
    queryKey: pluginKeys.configuration(id),
    queryFn: ({ signal }) =>
      apiGet(`/api/v1/plugins/${encodeURIComponent(id)}/configuration`, configurationSchema, signal),
    enabled,
    // Always read the current saved values when the dialog opens.
    gcTime: 0,
    retry: false,
  });
}

// The server's reply is the new truth: nothing is shown as installed or saved
// until it answers, so a failed request can never look like it worked.
export function useInstallPlugin(id: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => sendJson("POST", `/api/v1/plugins/${encodeURIComponent(id)}/install`, {}, pluginDetailSchema),
    onSuccess: async (detail) => {
      client.setQueryData(pluginKeys.detail(id), detail);
      await client.invalidateQueries({ queryKey: pluginKeys.list });
    },
  });
}

export function useSaveConfiguration(id: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: ConfigurationInput) =>
      sendJson("PUT", `/api/v1/plugins/${encodeURIComponent(id)}/configuration`, input, pluginDetailSchema),
    onSuccess: async (detail) => {
      client.setQueryData(pluginKeys.detail(id), detail);
      await Promise.all([
        client.invalidateQueries({ queryKey: pluginKeys.list }),
        client.invalidateQueries({ queryKey: pluginKeys.configuration(id) }),
      ]);
    },
  });
}