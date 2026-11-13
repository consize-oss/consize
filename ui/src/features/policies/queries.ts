"use client";

import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { ApiError, apiGet } from "@/lib/api-client";

// Backend-owned wording: the API says "30% headroom / Max 25% per cycle"; the
// UI never reconstructs rule text from numbers it doesn't own.
const textPair = z.object({ summary: z.string(), detail: z.string().optional() });

export const policySchema = z.object({
  id: z.string(),
  name: z.string(),
  version: z.string(),
  owner_team: z.string(),
  kind: z.enum(["guardrail", "automation", "database", "gitops", "storage", "ai"]),
  status: z.enum(["active", "draft", "disabled"]),
  scope: textPair.extend({ environments: z.array(z.string()) }),
  safety: textPair,
  action_path: z.object({
    method: z.enum(["gitops_pr", "terraform_pr", "direct_apply", "scheduled", "gateway_route"]),
    label: z.string(),
    approval: z.string(),
  }),
  verification: textPair,
  coverage: z.object({
    matched: z.number().int().nonnegative(),
    matched_ratio: z.number().min(0).max(1),
    // The backend decides what counts as a gap; the UI only displays it.
    needs_attention: z.boolean(),
  }),
  modified_at: z.string().datetime(),
  modified_by: z.string(),
});
export type Policy = z.infer<typeof policySchema>;

export const policySummarySchema = z.object({
  evaluated_at: z.string().datetime(),
  window: z.string(),
  active: z.number().int().nonnegative(),
  drafts: z.number().int().nonnegative(),
  coverage: z.object({ covered: z.number().int().nonnegative(), total: z.number().int().nonnegative() }),
  exceptions: z.object({
    open: z.number().int().nonnegative(),
    expiring_soon: z.number().int().nonnegative(),
    expiring_window_days: z.number().int().positive(),
  }),
  // Windowed by `window`; everything else in the summary is a current state.
  blocked_changes: z.number().int().nonnegative(),
  conflicts: z.number().int().nonnegative(),
});
export type PolicySummary = z.infer<typeof policySummarySchema>;

export const policyListSchema = z.object({
  policies: z.array(policySchema),
  summary: policySummarySchema,
});

export const POLICY_QUERY_KEY = ["policies", "inventory"] as const;

export function usePolicyList(window?: string) {
  return useQuery({
    queryKey: [...POLICY_QUERY_KEY, { window: window ?? null }],
    queryFn: ({ signal }) =>
      apiGet(`/api/v1/policies${window ? `?window=${encodeURIComponent(window)}` : ""}`, policyListSchema, signal),
    // Keep the table steady while the date window changes the summary.
    placeholderData: (previous) => previous,
    retry: (count, error) =>
      !(error instanceof ApiError && [401, 403, -1].includes(error.status)) && count < 1,
    refetchInterval: 30_000,
  });
}