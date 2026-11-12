"use client";

export type SegmentedView<T extends string> = {
  id: T;
  label: string;
  count: number;
  /** Highlight the count (e.g. an "Attention" view). */
  warn?: boolean;
};

// Segmented view toggle (the All / Attention / Unmonitored group on Resources).
export function SegmentedViews<T extends string>({
  label,
  views,
  active,
  onSelect,
  controls,
  loading,
  disabled,
}: {
  label: string;
  views: SegmentedView<T>[];
  active: T;
  onSelect: (id: T) => void;
  controls: string;
  loading: boolean;
  disabled: boolean;
}) {
  return (
    <div
      role="group"
      aria-label={label}
      aria-busy={loading}
      className="inline-flex max-w-full flex-wrap items-center gap-1 rounded-btn border border-app-border bg-app-bg p-1"
    >
      {views.map((view) => (
        <button
          key={view.id}
          type="button"
          disabled={disabled}
          aria-pressed={active === view.id}
          aria-controls={controls}
          onClick={() => onSelect(view.id)}
          className={
            "inline-flex h-7 cursor-pointer items-center gap-1.5 whitespace-nowrap rounded px-2 text-xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed " +
            (active === view.id ? "bg-ok-bg font-medium text-ok-strong" : "text-fg-secondary hover:bg-app-surface-alt")
          }
        >
          {view.label}
          <span className={"font-num text-[11px] " + (view.warn ? "text-warn-fg" : "")}>
            {disabled ? "\u2014" : view.count}
          </span>
        </button>
      ))}
    </div>
  );
}