"use client";

import { useId, useState } from "react";
import { RotateCcw, Search } from "lucide-react";
import { FilterSelect } from "@/components/ui/FilterSelect";

export type RecommendationFilterValues = {
  q: string;
  environment: string;
  risk: string;
  owner: string;
  provider: string;
  status: string;
};

type RecommendationsFiltersProps = {
  filters: RecommendationFilterValues;
  options: { environments: string[]; owners: string[]; providers: string[] };
  onChange: (key: keyof RecommendationFilterValues, value: string) => void;
  onReset: () => void;
};

const STATUSES = ["actionable", "pending_approval", "planned", "blocked", "executed", "superseded"];

export function RecommendationsFilters({
  filters,
  options,
  onChange,
  onReset,
}: RecommendationsFiltersProps) {
  // Keep typing local; Enter or the search icon applies the completed search.
  const [searchDraft, setSearchDraft] = useState(filters.q);
  const [moreFiltersOpen, setMoreFiltersOpen] = useState(Boolean(filters.provider));
  const moreFiltersId = useId();
  const hasFilters = Object.values(filters).some(Boolean);
  const extraCount = Number(Boolean(filters.provider))
    + Number(Boolean(filters.status) && !["pending_approval", "blocked"].includes(filters.status));

  // Keep the field in sync with Reset and browser Back/Forward navigation.
  const [appliedQuery, setAppliedQuery] = useState(filters.q);
  if (appliedQuery !== filters.q) {
    setAppliedQuery(filters.q);
    setSearchDraft(filters.q);
  }

  return (
    <div className="border-b border-app-border">
      <div className="flex flex-col gap-3 px-4 py-3 xl:flex-row xl:items-center xl:justify-between">
        <form
          role="search"
          aria-label="Recommendation search"
          className="relative min-w-0 flex-1 xl:max-w-[440px]"
          onSubmit={(event) => {
            event.preventDefault();
            onChange("q", searchDraft.trim());
          }}
        >
          <button
            type="submit"
            aria-label="Apply recommendation search"
            title="Search recommendations"
            className="absolute inset-y-0 left-0 flex w-9 cursor-pointer items-center justify-center rounded-l-md text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
          >
            <Search aria-hidden="true" className="h-3.5 w-3.5" />
          </button>
          <input
            type="search"
            name="q"
            aria-label="Search recommendations"
            title="Press Enter to search"
            placeholder="Search resource, owner, namespace, or tag..."
            value={searchDraft}
            onChange={(event) => {
              const value = event.target.value;
              setSearchDraft(value);
              // Clearing the field also clears its applied search.
              if (!value) onChange("q", "");
            }}
            className="h-8 w-full rounded-md border border-app-border bg-app-bg pl-9 pr-3 text-xs text-fg placeholder:text-fg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
          />
        </form>

        <div className="flex flex-wrap items-center gap-2">
          <FilterSelect
            label="Environment"
            allLabel="All environments"
            value={filters.environment}
            options={options.environments}
            onChange={(value) => onChange("environment", value)}
          />
          <FilterSelect
            label="Risk"
            allLabel="All risks"
            value={filters.risk}
            options={["low", "medium", "high"]}
            onChange={(value) => onChange("risk", value)}
          />
          <FilterSelect
            label="Owner"
            allLabel="All owners"
            value={filters.owner}
            options={options.owners}
            formatOption={(value) => value}
            onChange={(value) => onChange("owner", value)}
          />
          <button
            type="button"
            aria-expanded={moreFiltersOpen}
            aria-controls={moreFiltersId}
            onClick={() => setMoreFiltersOpen((open) => !open)}
            className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md border border-app-border bg-app-surface px-3 text-xs font-medium text-fg hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
          >
            More filters
            {extraCount > 0 && <span className="rounded bg-accent-400/15 px-1 text-[10px] text-accent-400">{extraCount}</span>}
          </button>
          <button
            type="button"
            disabled={!hasFilters && !searchDraft}
            onClick={() => {
              setSearchDraft("");
              setMoreFiltersOpen(false);
              onReset();
            }}
            className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md px-1 text-xs text-fg-secondary hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-default disabled:opacity-50"
          >
            <RotateCcw aria-hidden="true" className="h-3.5 w-3.5" />
            Reset Filters
          </button>
        </div>
      </div>

      {moreFiltersOpen && (
        <div id={moreFiltersId} className="flex flex-wrap items-center gap-3 border-t border-app-border bg-app-bg/50 px-4 py-3">
          <FilterSelect
            label="Provider"
            allLabel="All providers"
            value={filters.provider}
            options={options.providers}
            onChange={(value) => onChange("provider", value)}
          />
          <FilterSelect
            label="Status"
            allLabel="All statuses"
            value={filters.status}
            options={STATUSES}
            onChange={(value) => onChange("status", value)}
          />
        </div>
      )}
    </div>
  );
}
