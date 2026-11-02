"use client";

import { ArrowRight } from "lucide-react";
import { useResourceInventory } from "./resourceInventory";

const ATTENTION_CATEGORIES = [
  { id: "unowned", label: "unowned", dotClass: "bg-amber-400" },
  { id: "stale", label: "stale telemetry", dotClass: "bg-rose-400" },
  { id: "policy", label: "policy gaps", dotClass: "bg-orange-400" },
  { id: "unsupported", label: "action unsupported", dotClass: "bg-sky-400" },
] as const;

export function ResourcesAttentionStrip() {
  const { summary, isLoading, isError, reviewAllGaps } = useResourceInventory();
  const available = !isLoading && !isError;

  return (
    <section
      aria-label="Coverage requiring attention" aria-busy={isLoading}
      className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-btn border border-app-border bg-app-surface px-4 py-3"
    >
      <h2 className="text-xs font-medium text-fg">Coverage requiring attention</h2>
      <ul className="flex flex-1 flex-wrap items-center gap-x-4 gap-y-2 text-[11px] text-fg-secondary">
        {ATTENTION_CATEGORIES.map(({ id, label, dotClass }) => (
          <li key={id} className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className={"h-1.5 w-1.5 shrink-0 rounded-full " + dotClass} />
            <span className="font-num">{available ? (summary.attention[id] ?? "\u2014") : "\u2014"}</span>
            <span>{label}</span>
          </li>
        ))}
      </ul>
      <button
        type="button" disabled={!available} onClick={reviewAllGaps} aria-controls="resource-inventory-results"
        className="inline-flex shrink-0 cursor-pointer items-center gap-1 rounded text-[11px] font-medium text-ok-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed"
      >
        Review all gaps
        <ArrowRight aria-hidden="true" className="h-3 w-3" />
      </button>
    </section>
  );
}
