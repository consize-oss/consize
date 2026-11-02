"use client";

import { useResourceInventory } from "./resourceInventory";
import { ResourcesFilters } from "./ResourcesFilters";
import { ResourcesTable } from "./ResourcesTable";
import { ResourcesPagination } from "./ResourcesPagination";

const INVENTORY_VIEWS = [
  { id: "all", label: "All" },
  { id: "attention", label: "Attention" },
  { id: "unmonitored", label: "Unmonitored" },
] as const;

export function ResourcesInventoryPanel() {
  const { counts, view: activeView, selectView, isLoading, isError } = useResourceInventory();
  const available = !isLoading && !isError;
  return (
    <section
      aria-labelledby="resource-inventory-heading"
      className="min-w-0 rounded-card border border-app-border bg-app-surface shadow-card"
    >
      <div className="flex flex-wrap items-start justify-between gap-3 px-4 pb-3 pt-4">
        <div className="min-w-0 space-y-1">
          <h2 id="resource-inventory-heading" className="text-sm font-semibold text-fg">
            Infrastructure inventory
          </h2>
          <p className="text-xs leading-4 text-fg-muted">
            All resources discovered from the infrastructure scope above.
          </p>
        </div>

        <div
          role="group"
          aria-label="Resource inventory views"
          aria-busy={isLoading}
          className="inline-flex max-w-full flex-wrap items-center gap-1 rounded-btn border border-app-border bg-app-bg p-1"
        >
          {INVENTORY_VIEWS.map((view) => (
            <button
              key={view.id}
              type="button"
              disabled={!available}
              aria-pressed={activeView === view.id}
              aria-controls="resource-inventory-results"
              onClick={() => selectView(view.id)}
              className={"inline-flex h-7 cursor-pointer items-center gap-1.5 whitespace-nowrap rounded px-2 text-xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed " + (activeView === view.id ? "bg-ok-bg font-medium text-ok-strong" : "text-fg-secondary hover:bg-app-surface-alt")}
            >
              {view.label}
              <span
                className={"font-num text-[11px] " + (view.id === "attention" ? "text-warn-fg" : "")}
              >
                {available ? counts[view.id] : "\u2014"}
              </span>
            </button>
          ))}
        </div>
      </div>

      <ResourcesFilters />
      <ResourcesTable />
      <ResourcesPagination />
    </section>
  );
}
