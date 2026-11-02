"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { getClientPage } from "@/lib/pagination";
import { reloadForMockConnectionFailure } from "@/lib/api-client";
import { useResourceList, useResourceSync } from "./queries";
import {
  buildResourceInventory,
  resourceInventoryCSV,
  RESOURCE_PAGE_SIZE,
  type ResourceFilters,
  type ResourceScope,
  type ResourceView,
} from "./resourceInventoryModel";

export { inventoryDetailsSchema, RESOURCE_PAGE_SIZE } from "./resourceInventoryModel";
export type { ResourceInventoryDetails, ResourceInventoryItem } from "./resourceInventoryModel";

const LOCAL_KEYS = ["q", "environment", "owner", "coverage", "telemetry", "optimization", "view"] as const;

export function useResourceInventory() {
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();
  const query = useResourceList();
  const sync = useResourceSync();
  const filters: ResourceFilters = {
    q: searchParams.get("q") ?? "",
    environment: searchParams.get("environment") ?? "",
    owner: searchParams.get("owner") ?? "",
    coverage: searchParams.get("coverage") ?? "",
    telemetry: searchParams.get("telemetry") ?? "",
    optimization: searchParams.get("optimization") ?? "",
  };
  const selectedView = searchParams.get("view");
  const view: ResourceView = selectedView === "attention" || selectedView === "unmonitored" ? selectedView : "all";
  const scope: ResourceScope = {
    provider: searchParams.get("provider") || undefined,
    source: searchParams.get("source") || undefined,
    region: searchParams.get("region") || undefined,
    type: searchParams.get("type") || undefined,
  };
  const inventory = buildResourceInventory(query.data?.resources ?? [], scope, filters, view, Number(searchParams.get("page")));
  const hasLocalFilters = Object.values(filters).some(Boolean) || view !== "all";

  function updateFilters(changes: Record<string, string>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(changes)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    params.delete("page");
    params.delete("cursor");
    const queryString = params.toString();
    if (queryString !== searchParams.toString()) {
      router.replace(queryString ? pathname + "?" + queryString : pathname, { scroll: false });
    }
  }

  function changeFilter(key: keyof ResourceFilters, value: string) {
    updateFilters({ [key]: value });
  }

  function selectView(nextView: ResourceView) {
    updateFilters({ view: nextView === "all" ? "" : nextView });
  }

  function resetFilters() {
    updateFilters(Object.fromEntries(LOCAL_KEYS.map((key) => [key, ""])));
  }

  function reviewAllGaps() {
    // Match the scope-wide attention strip, even if the table had narrower local filters.
    updateFilters({ q: "", environment: "", owner: "", coverage: "", telemetry: "", optimization: "", view: "attention" });
  }

  function changePage(requestedPage: number) {
    const nextPage = getClientPage(inventory.filteredItems, requestedPage, RESOURCE_PAGE_SIZE).currentPage;
    if (nextPage === inventory.pagination.currentPage) return;
    const params = new URLSearchParams(searchParams.toString());
    params.delete("cursor");
    if (nextPage === 1) params.delete("page");
    else params.set("page", String(nextPage));
    const queryString = params.toString();
    router.push(queryString ? pathname + "?" + queryString : pathname, { scroll: false });
  }

  async function refresh() {
    sync.reset();
    const result = await query.refetch();
    return !result.isError;
  }

  async function syncInventory() {
    try {
      await sync.mutateAsync();
      return true;
    } catch {
      return false;
    }
  }

  function exportInventory() {
    if (inventory.filteredItems.length === 0) return 0;
    const csv = resourceInventoryCSV(inventory.filteredItems);
    const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = "resource-inventory.csv";
    document.body.appendChild(link);
    try {
      link.click();
    } finally {
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    }
    return inventory.filteredItems.length;
  }

  async function retry() {
    const result = await query.refetch();
    if (result.error) reloadForMockConnectionFailure(result.error);
  }

  return {
    ...inventory,
    asOf: query.dataUpdatedAt,
    items: inventory.pagination.items,
    filters,
    view,
    hasFilters: hasLocalFilters || Object.values(scope).some(Boolean),
    hasLocalFilters,
    changeFilter,
    selectView,
    resetFilters,
    reviewAllGaps,
    changePage,
    isLoading: query.isPending,
    isError: query.isError,
    error: query.error,
    isRetrying: query.isFetching,
    isSyncing: sync.isPending,
    syncError: sync.error,
    refresh,
    syncInventory,
    exportInventory,
    retry,
  };
}
