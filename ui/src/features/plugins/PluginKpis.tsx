"use client";

import type { PluginSummary } from "@/schemas";

const CARD = "rounded-card border border-app-border bg-app-surface p-4 shadow-card";
const CHIP = "rounded border px-2 py-1 text-[11px] font-semibold whitespace-nowrap";
const CHIP_OK = `${CHIP} border-ok-line bg-ok-bg text-ok-strong`;
const CHIP_WARN = `${CHIP} border-warn-line bg-warn-bg text-warn-fg`;

function Kpi({
  label,
  chip,
  value,
  footer,
  footerTone = "muted",
}: {
  label: string;
  chip: React.ReactNode;
  value: string;
  footer: string;
  footerTone?: "ok" | "muted" | "warn";
}) {
  const tone = { ok: "text-ok-strong", muted: "text-fg-muted", warn: "text-warn-fg" }[footerTone];
  return (
    <article className={CARD}>
      <div className="flex items-start justify-between gap-2">
        <p className="text-xs font-medium text-fg-secondary">{label}</p>
        {chip}
      </div>
      <div className="mt-5 text-[24px] font-semibold tracking-tight text-fg font-num">{value}</div>
      <div className={`mt-3 border-t border-app-border-subtle pt-3 text-xs ${tone}`}>{footer}</div>
    </article>
  );
}

export function PluginKpis({ summary }: { summary: PluginSummary }) {
  const { ingestion, actions, recommendations, trust } = summary;
  const untrusted = trust.total - trust.trusted;
  const freshPct = ingestion.fresh_ratio === null ? null : Math.round(ingestion.fresh_ratio * 100);

  return (
    <section aria-label="Plugin summary" className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3">
      <Kpi
        label="Active ingestion plugins"
        chip={
          <span className={ingestion.active < ingestion.total ? CHIP_WARN : CHIP_OK}>
            {ingestion.active} / {ingestion.total} active
          </span>
        }
        value={`${ingestion.active} connected`}
        footer={
          freshPct === null
            ? "Freshness unknown"
            : `Freshness: ${freshPct}% within ${ingestion.freshness_window_seconds}s`
        }
        footerTone={freshPct === 100 ? "ok" : freshPct === null ? "muted" : "warn"}
      />
      <Kpi
        label="Action capabilities"
        chip={
          actions.state === "ready" ? (
            <span className={`${CHIP} border-info-line bg-info-bg text-info-fg`}>Ready</span>
          ) : (
            <span className={CHIP_WARN}>Needs review</span>
          )
        }
        value={`${actions.paths} action paths`}
        footer={actions.summary}
      />
      <Kpi
        label="Recommendations produced"
        chip={<span className={CHIP_OK}>{recommendations.active} active</span>}
        value={`${recommendations.resources.toLocaleString("en-US")} resources`}
        footer={`${recommendations.active} proposals across connected infrastructure`}
      />
      <Kpi
        label="Package trust"
        chip={
          trust.assessed === false ? (
            <span className={CHIP_WARN}>Not assessed</span>
          ) : untrusted === 0 ? (
            <span className={CHIP_OK}>Verified</span>
          ) : (
            <span className={CHIP_WARN}>{untrusted} untrusted</span>
          )
        }
        value={trust.assessed === false ? "Unavailable" : `${trust.trusted} of ${trust.total} trusted`}
        footer={trust.assessed === false ? "Package trust is not supplied by the runtime API" : untrusted === 0 ? "Signatures and checksums valid" : "Review packages without a valid signature"}
        footerTone={trust.assessed === false ? "muted" : untrusted === 0 ? "ok" : "warn"}
      />
    </section>
  );
}