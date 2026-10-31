"use client";

import { useId, useState } from "react";
import { Search } from "lucide-react";
import { FilterSelect } from "@/components/ui/FilterSelect";
import { titleCase } from "@/lib/format";
import { useResourceInventory } from "./resourceInventory";
import { NO_OWNER, OPTIMIZATION_LABELS, TELEMETRY_LABELS } from "./resourceInventoryModel";

const COVERAGE_LABELS: Record<string, string> = {
  covered: "Covered", uncovered: "Missing coverage", unknown: "Unknown coverage",
};

export function ResourcesFilters() {
  const { filters, options, isLoading, isError, hasLocalFilters, changeFilter, resetFilters } = useResourceInventory();
  const [searchDraft, setSearchDraft] = useState(filters.q);
  const [appliedQuery, setAppliedQuery] = useState(filters.q);
  if (appliedQuery !== filters.q) {
    setAppliedQuery(filters.q);
    setSearchDraft(filters.q);
  }
  const extraSignature = filters.telemetry + "|" + filters.optimization;
  const extraCount = Number(Boolean(filters.telemetry)) + Number(Boolean(filters.optimization));
  const [moreFiltersOpen, setMoreFiltersOpen] = useState(extraCount > 0);
  const [appliedExtras, setAppliedExtras] = useState(extraSignature);
  if (appliedExtras !== extraSignature) {
    setAppliedExtras(extraSignature);
    if (extraCount > 0) setMoreFiltersOpen(true);
  }
  const moreFiltersId = useId();
  const disabled = isLoading || isError;

  return (
    <div className="border-b border-app-border">
      <div className="flex flex-col gap-3 px-4 pb-3 xl:flex-row xl:items-center xl:justify-between">
        <form role="search" aria-label="Resource search" className="relative min-w-0 flex-1 xl:max-w-[440px]"
          onSubmit={(event) => { event.preventDefault(); changeFilter("q", searchDraft.trim()); }}
        >
          <button type="submit" disabled={disabled} aria-label="Apply resource search" title="Search resources"
            className="absolute inset-y-0 left-0 flex w-9 cursor-pointer items-center justify-center rounded-l-md text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
          ><Search aria-hidden="true" className="h-3.5 w-3.5" /></button>
          <input type="search" name="q" aria-label="Search resources"
            placeholder="Search by resource, ID, owner, account, or tag..." title="Press Enter to search" disabled={disabled}
            value={searchDraft} onChange={(event) => {
              setSearchDraft(event.target.value);
              if (!event.target.value) changeFilter("q", "");
            }}
            className="h-8 w-full rounded-md border border-app-border bg-app-bg pl-9 pr-3 text-xs text-fg placeholder:text-fg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
          />
        </form>
        <div className="flex flex-wrap items-center gap-2">
          <FilterSelect label="Environment" allLabel="All environments" value={filters.environment} options={options.environments}
            formatOption={(value) => value === "datalake" ? "DataLake" : titleCase(value)}
            onChange={(value) => changeFilter("environment", value)} disabled={disabled} />
          <FilterSelect label="Owner" allLabel="All owners" value={filters.owner} options={options.owners}
            formatOption={(value) => value === NO_OWNER ? "No owner" : value}
            onChange={(value) => changeFilter("owner", value)} disabled={disabled} />
          <FilterSelect label="Coverage" allLabel="All coverage" value={filters.coverage} options={options.coverage}
            formatOption={(value) => COVERAGE_LABELS[value] ?? value}
            onChange={(value) => changeFilter("coverage", value)} disabled={disabled} />
          <button type="button" disabled={disabled} aria-expanded={moreFiltersOpen} aria-controls={moreFiltersId}
            onClick={() => setMoreFiltersOpen((open) => !open)}
            className="inline-flex h-8 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-app-border bg-app-surface px-3 text-xs font-medium text-fg hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
          >
            More filters
            {extraCount > 0 && <span className="rounded bg-ok-bg px-1 text-[10px] text-ok-strong">{extraCount}</span>}
          </button>
          <button type="button" disabled={!hasLocalFilters && !searchDraft}
            onClick={() => { setSearchDraft(""); setMoreFiltersOpen(false); resetFilters(); }}
            className="inline-flex h-8 cursor-pointer items-center justify-center rounded-md px-1 text-xs text-fg-secondary hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-default disabled:opacity-50"
          >Reset Filters</button>
        </div>
      </div>
      {moreFiltersOpen && (
        <div id={moreFiltersId} role="group" aria-label="Additional resource filters"
          className="flex flex-wrap items-center gap-3 border-t border-app-border bg-app-bg/50 px-4 py-3"
        >
          <FilterSelect label="Telemetry" allLabel="All telemetry" value={filters.telemetry} options={options.telemetry}
            formatOption={(value) => TELEMETRY_LABELS[value as keyof typeof TELEMETRY_LABELS] ?? titleCase(value)}
            onChange={(value) => changeFilter("telemetry", value)} disabled={disabled} />
          <FilterSelect label="Optimization status" allLabel="All optimization statuses" value={filters.optimization} options={options.optimization}
            formatOption={(value) => OPTIMIZATION_LABELS[value as keyof typeof OPTIMIZATION_LABELS] ?? titleCase(value)}
            onChange={(value) => changeFilter("optimization", value)} disabled={disabled} />
        </div>
      )}
    </div>
  );
}
