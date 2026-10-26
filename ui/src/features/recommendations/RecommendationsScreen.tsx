"use client";

import { useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { prepareApiRetry, reloadForMockConnectionFailure } from "@/lib/api-client";
import { useQueryClient } from "@tanstack/react-query";
import { queryKeys, useRecommendationList } from "../overview/queries";
import { RecommendationsHeader } from "./RecommendationsHeader";
import { RecommendationsFilters, type RecommendationFilterValues } from "./RecommendationsFilters";
import {
  RecommendationsStatusTabs,
  type RecommendationCategory,
  type RecommendationCategoryCounts,
} from "./RecommendationsStatusTabs";
import { RecommendationsTable } from "./RecommendationsTable";
import { getRecommendationPage, RecommendationsPagination } from "./RecommendationsPagination";

function unique(values: string[]) {
  return Array.from(new Set(values)).sort();
}

export function RecommendationsScreen() {
  const queryClient = useQueryClient();
  const [retrying, setRetrying] = useState(false);
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();
  const filters: RecommendationFilterValues = {
    q: searchParams.get("q") ?? "",
    environment: searchParams.get("environment") ?? "",
    risk: searchParams.get("risk") ?? "",
    owner: searchParams.get("owner") ?? "",
    provider: searchParams.get("provider") ?? "",
    status: searchParams.get("status") ?? "",
  };

  // Fetch matching rows once so the table and pagination use the same results.
  const results = useRecommendationList(Object.fromEntries(
    Object.entries(filters).map(([key, value]) => [key, value || undefined]),
  ));
  const resultItems = results.data?.items ?? [];
  const pagination = getRecommendationPage(resultItems, Number(searchParams.get("page")));
  const resultsLoading = results.isPending || results.isPlaceholderData;
  // The current MSW response contains the complete filtered list. A cursor-based
  // backend needs its own totals/navigation; never invent a total from one server page.
  const hasCompleteResults = results.data?.page.has_more === false;

  // Count in the current search/scope, before applying a category's status/risk.
  const { data, isPending, isError } = useRecommendationList({
    q: filters.q || undefined,
    environment: filters.environment || undefined,
    provider: filters.provider || undefined,
    owner: filters.owner || undefined,
  });
  // Keep dropdown options stable when filters narrow the visible results.
  const facets = useRecommendationList();
  const facetItems = facets.data?.items ?? [];
  const options = {
    environments: unique(facetItems.map((item) => item.resource.environment)),
    owners: unique(facetItems.map((item) => item.owner)),
    providers: unique(facetItems.map((item) => item.resource.provider)),
  };

  // The mock returns a complete list. A paginated backend must supply totals;
  // counting only one page would produce misleading badges.
  const items = data && !isError && !data.page.has_more ? data.items : undefined;
  const counts: RecommendationCategoryCounts = {
    all: items?.length ?? null,
    approvals: items?.filter((item) => item.status === "pending_approval").length ?? null,
    "high-risk": items?.filter((item) => item.risk === "high").length ?? null,
    blocked: items?.filter((item) => item.status === "blocked").length ?? null,
  };

  // Custom combinations from More filters need not match a single category.
  let activeCategory: RecommendationCategory | null = null;
  if (!filters.status && !filters.risk) activeCategory = "all";
  else if (filters.status === "pending_approval" && !filters.risk) activeCategory = "approvals";
  else if (filters.status === "blocked" && !filters.risk) activeCategory = "blocked";
  else if (!filters.status && filters.risk === "high") activeCategory = "high-risk";

  async function retryRecommendations() {
    if (retrying) return;
    setRetrying(true);
    try {
      await prepareApiRetry();
      // Recover the table, category counts, header, and dropdown options together.
      await queryClient.refetchQueries(
        {
          queryKey: queryKeys.recommendations.all,
          type: "active",
        },
        { throwOnError: true },
      );
    } catch (error) {
      // Refresh only if local mocks lost their connection after inactivity.
      reloadForMockConnectionFailure(error);
    } finally {
      setRetrying(false);
    }
  }

  function updateUrl(params: URLSearchParams) {
    params.delete("cursor");
    params.delete("page");
    const query = params.toString();
    router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
  }

  function changePage(page: number) {
    const params = new URLSearchParams(searchParams.toString());
    params.delete("cursor");
    if (page === 1) params.delete("page");
    else params.set("page", String(page));
    const query = params.toString();
    // Keep filters in the URL and allow browser Back to revisit a previous page.
    router.push(query ? `${pathname}?${query}` : pathname, { scroll: false });
  }

  function changeFilter(key: keyof RecommendationFilterValues, value: string) {
    const params = new URLSearchParams(searchParams.toString());
    if ((params.get(key) ?? "") === value) return;
    if (value) params.set(key, value);
    else params.delete(key);
    updateUrl(params);
  }

  function resetFilters() {
    const params = new URLSearchParams(searchParams.toString());
    for (const key of Object.keys(filters)) params.delete(key);
    updateUrl(params);
  }

  function selectCategory(category: RecommendationCategory) {
    const params = new URLSearchParams(searchParams.toString());

    // Replace the category while preserving the search and other filters.
    params.delete("status");
    params.delete("risk");
    if (category === "approvals") params.set("status", "pending_approval");
    if (category === "blocked") params.set("status", "blocked");
    if (category === "high-risk") params.set("risk", "high");
    updateUrl(params);
  }

  return (
    <div className="space-y-4">
      <RecommendationsHeader />
      <section
        aria-labelledby="recommendations-card-title"
        className="overflow-hidden rounded-lg border border-app-border bg-app-surface"
      >
        <div className="flex flex-col items-start gap-3 border-b border-app-border px-4 py-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 id="recommendations-card-title" className="text-sm font-semibold text-fg">
              All recommendations
            </h2>
            <p className="mt-1 text-xs leading-4 text-fg-muted">
              Review evidence, change risk, status, and available action paths.
            </p>
          </div>
          <RecommendationsStatusTabs
            activeCategory={activeCategory}
            counts={counts}
            onSelect={selectCategory}
            isLoading={isPending}
          />
        </div>
        <RecommendationsFilters
          filters={filters}
          options={options}
          onChange={changeFilter}
          onReset={resetFilters}
        />
        <div id="recommendation-results">
          <RecommendationsTable
            items={hasCompleteResults ? pagination.items : resultItems}
            isLoading={resultsLoading}
            isError={results.isError}
            isRetrying={retrying || results.isFetching}
            hasFilters={Object.values(filters).some(Boolean)}
            onRetry={() => void retryRecommendations()}
          />
          {!resultsLoading && !results.isError && hasCompleteResults && (
            <RecommendationsPagination
              visibleCount={pagination.items.length}
              totalItems={pagination.totalItems}
              currentPage={pagination.currentPage}
              totalPages={pagination.totalPages}
              onPageChange={changePage}
            />
          )}
        </div>
      </section>
    </div>
  );
}
