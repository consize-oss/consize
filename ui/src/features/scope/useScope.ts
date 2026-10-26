"use client";

import { useCallback } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

// Infrastructure scope + date window live in the URL so a link reproduces
// the same view (PRD §6.2). Only endpoints that support a param receive it.
export const SCOPE_KEYS = ["provider", "source", "region", "type"] as const;
export type ScopeKey = (typeof SCOPE_KEYS)[number];
export type Scope = Partial<Record<ScopeKey, string>>;

export const WINDOWS = [
  { value: "P1D", label: "Last 24 hours", phrase: "previous 24 hours" },
  { value: "P7D", label: "Last 7 days", phrase: "previous 7 days" },
  { value: "P30D", label: "Last 30 days", phrase: "previous 30 days" },
] as const;
export const DEFAULT_WINDOW = "P30D";

// Omit the default window so the unscoped summary shares one cache entry
// (sidebar badge + overview) instead of fetching twice.
export function summaryParamsOf(scope: Scope, range: string) {
  return { ...scope, window: range === DEFAULT_WINDOW ? undefined : range };
}

export function useScope() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const queryString = searchParams.toString();

  const scope: Scope = {};
  for (const key of SCOPE_KEYS) {
    const v = searchParams.get(key);
    if (v) scope[key] = v;
  }
  const range = searchParams.get("window") ?? DEFAULT_WINDOW;

  // Merge changes into the current URL ("" / undefined removes a param).
  const update = useCallback(
    (changes: Record<string, string | undefined>) => {
      const p = new URLSearchParams(queryString);
      for (const [k, v] of Object.entries(changes)) {
        if (v) p.set(k, v);
        else p.delete(k);
      }
      const qs = p.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [queryString, pathname, router],
  );

  // Links to other pages keep the active scope and window.
  const carry = useCallback(
    (href: string) => {
      const p = new URLSearchParams();
      for (const key of SCOPE_KEYS) {
        const v = new URLSearchParams(queryString).get(key);
        if (v) p.set(key, v);
      }
      const w = new URLSearchParams(queryString).get("window");
      if (w && w !== DEFAULT_WINDOW) p.set("window", w);
      const qs = p.toString();
      if (!qs) return href;
      return href + (href.includes("?") ? "&" : "?") + qs;
    },
    [queryString],
  );

  return { scope, range, update, carry, isScoped: Object.keys(scope).length > 0 };
}