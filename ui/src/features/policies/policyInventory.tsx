"use client";

import { createContext, useContext, useMemo } from "react";
import { downloadCsv } from "@/lib/csv";
import { useUrlFilters } from "@/lib/useUrlFilters";
import { DEFAULT_WINDOW, WINDOWS, useScope } from "../scope/useScope";
import { usePolicyList, type Policy } from "./queries";
import {
  FILTER_KEYS,
  POLICY_PAGE_SIZE,
  buildPolicyInventory,
  policyMatrixCSV,
  type PolicyFilters,
} from "./policyInventoryModel";

const NO_POLICIES: readonly Policy[] = [];

function useValue() {
  const { range, carry, isScoped } = useScope();
  const query = usePolicyList(range === DEFAULT_WINDOW ? undefined : range);
  const { searchParams, update, goToPage } = useUrlFilters();

  const filters: PolicyFilters = {
    q: searchParams.get("q") ?? "",
    status: searchParams.get("status") ?? "",
    environment: searchParams.get("environment") ?? "",
    path: searchParams.get("path") ?? "",
    owner: searchParams.get("owner") ?? "",
    coverage: searchParams.get("coverage") ?? "",
  };
  const page = Number(searchParams.get("page"));
  const policies = query.data?.policies ?? NO_POLICIES;

  // Computed once per change, not once per section.
  const inventory = useMemo(
    () => buildPolicyInventory(policies, filters, page),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [policies, filters.q, filters.status, filters.environment, filters.path, filters.owner, filters.coverage, page],
  );
  const hasFilters = Object.values(filters).some(Boolean);

  return {
    ...inventory,
    items: inventory.pagination.items,
    summary: query.data?.summary,
    runtimePolicy: query.data?.runtimePolicy,
    asOf: query.dataUpdatedAt,
    windowLabel: WINDOWS.find((w) => w.value === range)?.label ?? "Last 30 days",
    filters,
    hasFilters,
    isScoped,
    carry,
    isLoading: query.isPending,
    isError: query.isError,
    isRetrying: query.isFetching,
    changeFilter: (key: string, value: string) => update({ [key]: value }),
    resetFilters: () => update(Object.fromEntries(FILTER_KEYS.map((key) => [key, ""]))),
    changePage: (requested: number) => {
      const next = Math.min(Math.max(1, requested), inventory.pagination.totalPages);
      if (next !== inventory.pagination.currentPage) goToPage(next);
    },
    refresh: async () => !(await query.refetch()).isError,
    retry: async () => void (await query.refetch()),
    exportMatrix: () => {
      if (inventory.filteredItems.length === 0) return 0;
      downloadCsv("policy-matrix.csv", policyMatrixCSV(inventory.filteredItems));
      return inventory.filteredItems.length;
    },
  };
}

type Value = ReturnType<typeof useValue>;
const Context = createContext<Value | null>(null);

export function PolicyInventoryProvider({ children }: { children: React.ReactNode }) {
  return <Context.Provider value={useValue()}>{children}</Context.Provider>;
}

export function usePolicyInventory() {
  const value = useContext(Context);
  if (!value) throw new Error("usePolicyInventory must be used inside <PolicyInventoryProvider>");
  return value;
}

export { POLICY_PAGE_SIZE };