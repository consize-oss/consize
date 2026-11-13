"use client";

import { useId, useState } from "react";
import { Search } from "lucide-react";
import { FilterSelect } from "@/components/ui/FilterSelect";

export type FilterSpec = {
  key: string;
  label: string;
  allLabel: string;
  value: string;
  options: string[];
  formatOption: (value: string) => string;
};

// Search + select filters + collapsible "More filters" + Reset
// (the Resources filter bar, generalized so other list pages reuse it).
export function InventoryFilters({
  searchLabel,
  searchPlaceholder,
  searchValue,
  onSearch,
  filters,
  extraFilters = [],
  onFilterChange,
  onReset,
  hasFilters,
  disabled,
}: {
  searchLabel: string;
  searchPlaceholder: string;
  /** The search text currently applied (from the URL). */
  searchValue: string;
  onSearch: (value: string) => void;
  filters: FilterSpec[];
  extraFilters?: FilterSpec[];
  onFilterChange: (key: string, value: string) => void;
  onReset: () => void;
  hasFilters: boolean;
  disabled: boolean;
}) {
  const [searchDraft, setSearchDraft] = useState(searchValue);
  const [appliedQuery, setAppliedQuery] = useState(searchValue);
  // Follow external changes (Reset, back button) without an effect.
  if (appliedQuery !== searchValue) {
    setAppliedQuery(searchValue);
    setSearchDraft(searchValue);
  }

  const extraCount = extraFilters.filter((f) => f.value).length;
  const extraSignature = extraFilters.map((f) => f.value).join("|");
  const [moreOpen, setMoreOpen] = useState(extraCount > 0);
  const [appliedExtras, setAppliedExtras] = useState(extraSignature);
  if (appliedExtras !== extraSignature) {
    setAppliedExtras(extraSignature);
    if (extraCount > 0) setMoreOpen(true);
  }
  const moreId = useId();

  return (
    <div className="border-b border-app-border">
      <div className="flex flex-col gap-3 px-4 pb-3 xl:flex-row xl:items-center xl:justify-between">
        <form
          role="search"
          aria-label={searchLabel}
          className="relative min-w-0 flex-1 xl:max-w-[440px]"
          onSubmit={(event) => {
            event.preventDefault();
            onSearch(searchDraft.trim());
          }}
        >
          <button
            type="submit"
            disabled={disabled}
            aria-label={"Apply " + searchLabel.toLowerCase()}
            title="Search"
            className="absolute inset-y-0 left-0 flex w-9 cursor-pointer items-center justify-center rounded-l-md text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
          >
            <Search aria-hidden="true" className="h-3.5 w-3.5" />
          </button>
          <input
            type="search"
            name="q"
            aria-label={searchLabel}
            placeholder={searchPlaceholder}
            title="Press Enter to search"
            disabled={disabled}
            value={searchDraft}
            onChange={(event) => {
              setSearchDraft(event.target.value);
              if (!event.target.value) onSearch("");
            }}
            className="h-8 w-full rounded-md border border-app-border bg-app-bg pl-9 pr-3 text-xs text-fg placeholder:text-fg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
          />
        </form>

        <div className="flex flex-wrap items-center gap-2">
          {filters.map((f) => (
            <FilterSelect
              key={f.key}
              label={f.label}
              allLabel={f.allLabel}
              value={f.value}
              options={f.options}
              formatOption={f.formatOption}
              onChange={(value) => onFilterChange(f.key, value)}
              disabled={disabled}
            />
          ))}
          {extraFilters.length > 0 && (
            <button
              type="button"
              disabled={disabled}
              aria-expanded={moreOpen}
              aria-controls={moreId}
              onClick={() => setMoreOpen((open) => !open)}
              className="inline-flex h-8 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-app-border bg-app-surface px-3 text-xs font-medium text-fg hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
            >
              More filters
              {extraCount > 0 && <span className="rounded bg-ok-bg px-1 text-[10px] text-ok-strong">{extraCount}</span>}
            </button>
          )}
          <button
            type="button"
            disabled={!hasFilters && !searchDraft}
            onClick={() => {
              setSearchDraft("");
              setMoreOpen(false);
              onReset();
            }}
            className="inline-flex h-8 cursor-pointer items-center justify-center rounded-md px-1 text-xs text-fg-secondary hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-default disabled:opacity-50"
          >
            Reset Filters
          </button>
        </div>
      </div>

      {moreOpen && extraFilters.length > 0 && (
        <div
          id={moreId}
          role="group"
          aria-label="Additional filters"
          className="flex flex-wrap items-center gap-3 border-t border-app-border bg-app-bg/50 px-4 py-3"
        >
          {extraFilters.map((f) => (
            <FilterSelect
              key={f.key}
              label={f.label}
              allLabel={f.allLabel}
              value={f.value}
              options={f.options}
              formatOption={f.formatOption}
              onChange={(value) => onFilterChange(f.key, value)}
              disabled={disabled}
            />
          ))}
        </div>
      )}
    </div>
  );
}