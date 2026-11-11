"use client";

import { InventoryFilters, type FilterSpec } from "@/components/ui/InventoryFilters";
import { Pagination } from "@/components/ui/Pagination";
import { SegmentedViews } from "@/components/ui/SegmentedViews";
import { titleCase } from "@/lib/format";
import { PoliciesTable } from "./PoliciesTable";
import { usePolicyInventory } from "./policyInventory";
import { COVERAGE_LABELS, STATUS_LABELS } from "./policyInventoryModel";

const RESULTS_ID = "policy-inventory-results";

export function PoliciesInventoryPanel() {
  const { filters, options, counts, pagination, hasFilters, isLoading, isError, isRetrying, changeFilter, resetFilters, changePage } =
    usePolicyInventory();
  const disabled = isLoading || isError;

  const environmentLabel = (v: string) => (v === "datalake" ? "DataLake" : titleCase(v));
  const selects: FilterSpec[] = [
    { key: "status", label: "Status", allLabel: "All statuses", value: filters.status, options: options.statuses,
      formatOption: (v) => STATUS_LABELS[v as keyof typeof STATUS_LABELS] ?? titleCase(v) },
    { key: "environment", label: "Environment", allLabel: "All environments", value: filters.environment,
      options: options.environments, formatOption: environmentLabel },
    { key: "path", label: "Action path", allLabel: "All action paths", value: filters.path, options: options.paths,
      formatOption: (v) => options.pathLabels[v] ?? titleCase(v) },
  ];
  const extras: FilterSpec[] = [
    { key: "owner", label: "Owner team", allLabel: "All teams", value: filters.owner, options: options.owners, formatOption: (v) => v },
    { key: "coverage", label: "Coverage", allLabel: "All coverage", value: filters.coverage, options: options.coverage,
      formatOption: (v) => COVERAGE_LABELS[v] ?? v },
  ];

  // The tabs and the Status select are two controls for ONE URL parameter, so
  // they can't disagree. (The Figma shows both; worth asking design if both are needed.)
  const view = filters.status === "active" || filters.status === "draft" ? filters.status : "all";

  return (
    <section aria-labelledby="policy-inventory-heading" className="min-w-0 rounded-card border border-app-border bg-app-surface shadow-card">
      <div className="flex flex-wrap items-start justify-between gap-3 px-4 pb-3 pt-4">
        <div className="min-w-0 space-y-1">
          <h2 id="policy-inventory-heading" className="text-sm font-semibold text-fg">Policy inventory</h2>
          <p className="text-xs leading-4 text-fg-muted">
            Policies are evaluated server-side against matching resources and proposed actions.
          </p>
        </div>
        <SegmentedViews
          label="Policy status views"
          views={[
            { id: "all", label: "All", count: counts.all },
            { id: "active", label: "Active", count: counts.active },
            { id: "draft", label: "Draft", count: counts.draft },
          ]}
          active={view}
          onSelect={(id) => changeFilter("status", id === "all" ? "" : id)}
          controls={RESULTS_ID}
          loading={isLoading}
          disabled={disabled}
        />
      </div>

      <InventoryFilters
        searchLabel="Search policies"
        searchPlaceholder="Search policy, scope, owner, or rule..."
        searchValue={filters.q}
        onSearch={(value) => changeFilter("q", value)}
        filters={selects}
        extraFilters={extras}
        onFilterChange={changeFilter}
        onReset={resetFilters}
        hasFilters={hasFilters}
        disabled={disabled}
      />

      <PoliciesTable resultsId={RESULTS_ID} />

      {!disabled && pagination.totalItems > 0 && (
        <Pagination
          currentPage={pagination.currentPage}
          totalPages={pagination.totalPages}
          totalItems={pagination.totalItems}
          startItem={pagination.startItem}
          endItem={pagination.endItem}
          itemLabel={pagination.totalItems === 1 ? "policy" : "policies"}
          navigationLabel="Policy pages"
          resultsId={RESULTS_ID}
          onPageChange={changePage}
          disabled={isRetrying}
        />
      )}
    </section>
  );
}