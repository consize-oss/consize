"use client";

import Link from "next/link";
import { ArrowRight } from "lucide-react";

export type AttentionItem = {
  id: string;
  /** Already pluralized by the caller, e.g. "resources uncovered". */
  label: string;
  /** null = the backend couldn't tell us; renders an em dash, never a 0. */
  count: number | null;
  dotClass: string;
};

type Action = { label: string; onClick?: () => void; href?: string; controls?: string };

// "Coverage requiring attention" strip (Resources layout, generalized).
export function AttentionStrip({
  title,
  ariaLabel,
  items,
  action,
  loading,
  unavailable,
}: {
  title: string;
  ariaLabel: string;
  items: AttentionItem[];
  action: Action;
  loading: boolean;
  unavailable: boolean;
}) {
  const available = !loading && !unavailable;
  const actionClass =
    "inline-flex shrink-0 cursor-pointer items-center gap-1 rounded text-[11px] font-medium text-ok-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500";

  return (
    <section
      aria-label={ariaLabel}
      aria-busy={loading}
      className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-btn border border-app-border bg-app-surface px-4 py-3"
    >
      <h2 className="text-xs font-medium text-fg">{title}</h2>
      <ul className="flex flex-1 flex-wrap items-center gap-x-4 gap-y-2 text-[11px] text-fg-secondary">
        {items.map((item) => (
          <li key={item.id} className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className={"h-1.5 w-1.5 shrink-0 rounded-full " + item.dotClass} />
            <span className="font-num">{available ? (item.count ?? "\u2014") : "\u2014"}</span>
            <span>{item.label}</span>
          </li>
        ))}
      </ul>
      {action.href && available ? (
        <Link href={action.href} className={actionClass}>
          {action.label}
          <ArrowRight aria-hidden="true" className="h-3 w-3" />
        </Link>
      ) : (
        <button
          type="button"
          disabled={!available}
          onClick={action.onClick}
          aria-controls={action.controls}
          className={actionClass + " disabled:cursor-not-allowed"}
        >
          {action.label}
          <ArrowRight aria-hidden="true" className="h-3 w-3" />
        </button>
      )}
    </section>
  );
}