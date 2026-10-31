"use client";

import { Pagination } from "@/components/ui/Pagination";
import { useResourceInventory } from "./resourceInventory";

export function ResourcesPagination() {
  const { pagination, isLoading, isError, isRetrying, changePage } = useResourceInventory();

  if (isLoading || isError || pagination.totalItems === 0) return null;

  return (
    <Pagination
      currentPage={pagination.currentPage}
      totalPages={pagination.totalPages}
      totalItems={pagination.totalItems}
      startItem={pagination.startItem}
      endItem={pagination.endItem}
      itemLabel={pagination.totalItems === 1 ? "resource" : "resources"}
      navigationLabel="Resource pages"
      resultsId="resource-inventory-results"
      onPageChange={changePage}
      disabled={isRetrying}
    />
  );
}
