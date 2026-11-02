"use client";

type PaginationProps = {
  currentPage: number;
  totalPages: number;
  totalItems: number;
  startItem: number;
  endItem: number;
  itemLabel: string;
  navigationLabel: string;
  resultsId: string;
  onPageChange: (page: number) => void;
  disabled?: boolean;
};

const BUTTON_CLASS = "inline-flex h-7 cursor-pointer items-center justify-center rounded-md border border-app-border bg-app-surface px-2.5 text-[11px] text-fg-secondary transition-colors hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-default disabled:opacity-40 disabled:hover:bg-app-surface";

// Display only: the feature owns its records, URL, page size, and page-change handler.
export function Pagination({
  currentPage,
  totalPages,
  totalItems,
  startItem,
  endItem,
  itemLabel,
  navigationLabel,
  resultsId,
  onPageChange,
  disabled = false,
}: PaginationProps) {
  const range = startItem === endItem ? String(startItem) : `${startItem}\u2013${endItem}`;

  return (
    <footer className="flex flex-wrap items-center justify-between gap-3 rounded-b-card border-t border-app-border bg-app-bg/40 px-4 py-2.5">
      <p role="status" aria-live="polite" aria-atomic="true" className="text-[11px] text-fg-secondary">
        Showing <span className="font-num">{range}</span> of <span className="font-num">{totalItems}</span> {itemLabel}
      </p>
      <nav aria-label={navigationLabel} className="flex items-center gap-2">
        <button
          type="button"
          disabled={disabled || currentPage <= 1}
          aria-controls={resultsId}
          onClick={() => onPageChange(currentPage - 1)}
          className={BUTTON_CLASS}
        >
          Previous
        </button>
        <span aria-label={`Page ${currentPage} of ${totalPages}`} className="min-w-9 text-center text-[11px] text-fg-secondary font-num">
          {currentPage} / {totalPages}
        </span>
        <button
          type="button"
          disabled={disabled || currentPage >= totalPages}
          aria-controls={resultsId}
          onClick={() => onPageChange(currentPage + 1)}
          className={BUTTON_CLASS}
        >
          Next
        </button>
      </nav>
    </footer>
  );
}
