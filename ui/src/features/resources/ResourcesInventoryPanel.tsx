"use client";

import { useRef, useState } from "react";
import { useResourceInventory, type ResourceInventoryItem } from "./resourceInventory";
import { ResourcesFilters } from "./ResourcesFilters";
import { ResourcesTable } from "./ResourcesTable";
import { ResourcesPagination } from "./ResourcesPagination";
import { ResourceGapDetailsPanel } from "./ResourceGapDetailsPanel";

const INVENTORY_VIEWS = [
  { id: "all", label: "All" },
  { id: "attention", label: "Attention" },
  { id: "unmonitored", label: "Unmonitored" },
] as const;

export function ResourcesInventoryPanel() {
  const { allItems, counts, view: activeView, selectView, isLoading, isError, asOf, refresh } = useResourceInventory();
  const [selectedResource, setSelectedResource] = useState<ResourceInventoryItem | null>(null);
  const reviewButton = useRef<HTMLButtonElement>(null);
  const inventoryHeading = useRef<HTMLHeadingElement>(null);
  const available = !isLoading && !isError;
  const reviewedResource = selectedResource && (allItems.find((resource) => resource.id === selectedResource.id) ?? selectedResource);

  function closeReview() {
    setSelectedResource(null);
    const focusTarget = reviewButton.current?.isConnected ? reviewButton.current : inventoryHeading.current;
    focusTarget?.focus({ preventScroll: true });
    reviewButton.current = null;
  }

  return (
    <section
      aria-labelledby="resource-inventory-heading"
      className="min-w-0 rounded-card border border-app-border bg-app-surface shadow-card"
    >
      <div className="flex flex-wrap items-start justify-between gap-3 px-4 pb-3 pt-4">
        <div className="min-w-0 space-y-1">
          <h2 ref={inventoryHeading} id="resource-inventory-heading" tabIndex={-1} className="text-sm font-semibold text-fg">
            Infrastructure inventory
          </h2>
          <p className="text-xs leading-4 text-fg-muted">
            Resources matching the selected view and filters.
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
      <div id="resource-inventory-results">
        <ResourcesTable onReview={(resource, button) => {
          reviewButton.current = button;
          setSelectedResource(resource);
        }} />
      </div>
      <ResourcesPagination />
      {reviewedResource && <ResourceGapDetailsPanel resource={reviewedResource} asOf={asOf} onRecheck={refresh} onClose={closeReview} />}
    </section>
  );
}
