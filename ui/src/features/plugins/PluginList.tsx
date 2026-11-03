"use client";

import Link from "next/link";
import { relativeTimeShort } from "@/lib/format";
import type { Plugin } from "@/schemas";
import { DOT, FACT_TEXT, KIND, PILL, pluginStatus } from "./status";

export function PluginList({
  items,
  selectedId,
  hrefFor,
  onSelect,
}: {
  items: Plugin[];
  selectedId: string | undefined;
  hrefFor: (id: string) => string;
  onSelect: (id: string) => void;
}) {
  return (
    <ul className="space-y-3">
      {items.map((p) => {
        const selected = p.id === selectedId;
        const status = pluginStatus(p);
        const kind = KIND[p.kind];
        const Icon = kind.icon;
        const healthyAge = status.tone === "ok" && p.engine_state === null;
        return (
          <li key={p.id}>
            {/* Whole card is a mouse target; the name link is the keyboard path. */}
            <article
              onClick={() => onSelect(p.id)}
              className={`cursor-pointer rounded-card border bg-app-surface p-4 shadow-card transition-colors ${
                selected
                  ? "border-emerald-500 ring-1 ring-emerald-500/20"
                  : "border-app-border hover:border-app-border-strong"
              }`}
            >
              <div className="flex items-start gap-3">
                <span className={`grid h-10 w-10 shrink-0 place-items-center rounded-btn border ${kind.tile}`}>
                  <Icon className="h-5 w-5" aria-hidden />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-sm font-semibold text-fg">
                      <Link
                        href={hrefFor(p.id)}
                        aria-current={selected ? "true" : undefined}
                        onClick={(e) => e.stopPropagation()}
                        className="rounded hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-500"
                      >
                        {p.display_name}
                      </Link>
                    </h2>
                    <span className="rounded border border-app-border bg-app-subcard px-2 py-0.5 text-[10px] text-fg-secondary">
                      {p.version}
                    </span>
                    <span className={`rounded border px-2 py-0.5 text-[10px] font-semibold ${kind.chip}`}>
                      {kind.label}
                    </span>
                    <span
                      className={`ml-auto inline-flex items-center gap-1.5 rounded border px-2 py-1 text-[11px] ${PILL[status.tone]}`}
                    >
                      {healthyAge && <i className={`h-1.5 w-1.5 rounded-full ${DOT[status.tone]}`} aria-hidden />}
                      {status.label}
                      {healthyAge && <> · {relativeTimeShort(p.last_checked_at)}</>}
                    </span>
                  </div>
                  <p className="mt-1 text-xs leading-5 text-fg-secondary">{p.description}</p>
                </div>
              </div>

              <dl className="mt-4 grid grid-cols-1 gap-3 border-t border-app-border-subtle pt-3 sm:grid-cols-3 sm:gap-4">
                {p.facts.map((f, i) => (
                  <div key={f.label} className="min-w-0">
                    <dt className="text-[10px] uppercase tracking-wider text-fg-muted">{f.label}</dt>
                    <dd
                      className={`mt-1 text-xs ${
                        f.tone === "default" ? (i === 0 ? "font-medium text-fg" : FACT_TEXT.default) : `font-medium ${FACT_TEXT[f.tone]}`
                      }`}
                    >
                      {f.value}
                    </dd>
                  </div>
                ))}
              </dl>
            </article>
          </li>
        );
      })}
    </ul>
  );
}