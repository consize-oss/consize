"use client";

import { useEffect, useState } from "react";
import { Download } from "lucide-react";
import { relativeTime } from "@/lib/format";
import { useDashboardSummary } from "../overview/queries";
import { summaryParamsOf, useScope } from "../scope/useScope";

type RecommendationsHeaderProps = {
  onExport?: () => void;
};

export function RecommendationsHeader({
  onExport,
}: RecommendationsHeaderProps) {
  const { scope, range } = useScope();
  const { data } = useDashboardSummary(summaryParamsOf(scope, range));
  const timestamp = data?.last_evaluated_at;
  const evaluatedAt = timestamp && Number.isFinite(Date.parse(timestamp)) ? timestamp : undefined;
  // Start the clock after mounting so server and browser initially render alike.
  const [now, setNow] = useState<number | null>(null);

  useEffect(() => {
    const updateTime = () => setNow(Date.now());
    updateTime();
    // Update the displayed age without making another network request.
    const timer = window.setInterval(updateTime, 10_000);
    // Stop this timer when leaving the page so timers do not accumulate.
    return () => window.clearInterval(timer);
  }, []);

  return (
    <header className="flex flex-col gap-2 border-b border-app-border pb-3 lg:flex-row lg:items-center lg:justify-between">
      <div className="min-w-0">
        <h1 className="text-2xl font-semibold tracking-tight text-fg">
          Recommendations
        </h1>
        <p className="mt-1 text-xs leading-4 text-fg-muted">
          Investigate, compare, and act on optimization recommendations across connected infrastructure.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3 lg:shrink-0">
        <p className="text-[11px] text-fg-muted">
          Last evaluated{" "}
          <time
            dateTime={evaluatedAt ?? undefined}
            title={evaluatedAt ? new Date(evaluatedAt).toLocaleString() : undefined}
          >
            {now === null || !evaluatedAt ? "—" : relativeTime(evaluatedAt, now)}
          </time>
        </p>
        {/* onExport will supply the download action; keep the button disabled until then. */}
        <button
          type="button"
          onClick={onExport}
          disabled={!onExport}
          className="inline-flex h-8 items-center justify-center gap-2 rounded-md border border-app-border bg-app-surface px-3 text-sm font-semibold text-fg transition-colors hover:bg-app-surface-alt focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
        >
          <Download aria-hidden="true" className="h-4 w-4" />
          Export CSV
        </button>
      </div>
    </header>
  );
}
