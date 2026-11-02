//MetricRow.tsx
// "use client";

// import { Activity, ShieldCheck, AlertTriangle, RotateCcw } from "lucide-react";
// import { useDashboardSummary } from "./queries";
// import type { Money } from "@/schemas";

// function formatMoney(m: Money) {
//   return `$${(m.amount_minor / 100).toLocaleString("en-US", { maximumFractionDigits: 0 })}`;
// }

// export function MetricsRow() {
  
//   const { data, isPending, isError, error } = useDashboardSummary();

//   if (isPending) {
//     return (
//       <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-4">
//         {Array.from({ length: 4 }).map((_, i) => (
//           <div key={i} className="h-28 rounded-xl bg-app-surface border border-app-border animate-pulse" />
//         ))}
//       </div>
//     );
//   }

//   if (isError) {
//     return (
//       <div className="rounded-xl bg-app-surface border border-app-border p-4" role="alert">
//         <p className="text-sm font-medium text-rose-300">Couldn&apos;t load summary metrics</p>
//         <p className="text-xs text-slate-400 mt-1">
//           {error instanceof Error ? error.message : "The summary endpoint is unavailable."}
//         </p>
//       </div>
//     );
//   }

//   return (
//     <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-4">
//       <MetricCard
//         label="Projected Savings"
//         value={formatMoney(data.projected_savings)}
//         suffix="/ mo"
//         icon={Activity}
//         footLeft={`${data.open_recommendations} open recs`}
//         tone="emerald"
//       />
//       <MetricCard
//         label="Verified Savings"
//         value={formatMoney(data.verified_operational_savings)}
//         suffix="/ mo"
//         icon={ShieldCheck}
//         footLeft="Operationally verified"
//         tone="emerald"
//       />
//       <MetricCard
//         label="Pending Approvals"
//         value={String(data.pending_approvals)}
//         suffix="actions"
//         icon={AlertTriangle}
//         footLeft="Requires SRE approval"
//         tone="amber"
//       />
//       <MetricCard
//         label="Rollback Rate"
//         value={`${data.rollback_rate.toFixed(1)}%`}
//         icon={RotateCcw}
//         footLeft={data.freshness === "stale" ? "Stale — last good value shown" : "Up to date"}
//         tone={data.freshness === "stale" ? "amber" : "emerald"}
//       />
//     </div>
//   );
// }

// function MetricCard({
//   label,
//   value,
//   suffix,
//   icon: Icon,
//   footLeft,
//   tone,
// }: {
//   label: string;
//   value: string;
//   suffix?: string;
//   icon: React.ComponentType<{ className?: string }>;
//   footLeft: string;
//   tone: "emerald" | "amber";
// }) {
//   const toneClasses =
//     tone === "amber"
//       ? "bg-amber-500/10 border-amber-500/20 text-amber-400"
//       : "bg-emerald-500/10 border-emerald-500/20 text-emerald-400";

//   return (
//     <article className="bg-app-surface border border-app-border rounded-xl p-5 flex flex-col justify-between">
//       <div className="flex items-center justify-between">
//         <span className="text-xs font-mono uppercase tracking-wider text-slate-400">{label}</span>
//         <div className={`w-7 h-7 rounded-lg border flex items-center justify-center ${toneClasses}`}>
//           <Icon className="w-4 h-4" />
//         </div>
//       </div>
//       <div className="my-3 text-2xl font-bold tracking-tight text-fg font-mono flex items-baseline gap-1.5">
//         {value}
//         {suffix && <span className="text-xs font-normal text-slate-400">{suffix}</span>}
//       </div>
//       <div className="pt-3 border-t border-app-border/80 text-xs text-slate-400">{footLeft}</div>
//     </article>
//   );
// }


"use client";

import Link from "next/link";
import { AlertCircle, CheckCircle2, RefreshCw, TrendingUp } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { formatMoney, formatMoneyCompact, relativeTime } from "@/lib/format";
import { WINDOWS, summaryParamsOf, useScope } from "../scope/useScope";
import { useDashboardSummary } from "./queries";

type Tone = "ok" | "warn";
const TILE: Record<Tone, string> = {
  ok: "bg-ok-bg border-ok-line text-ok-strong",
  warn: "bg-warn-bg border-warn-line text-warn-fg",
};
const CARD =
  "bg-app-surface border border-app-border rounded-btn p-4 flex flex-col justify-between shadow-card hover:border-app-border-strong transition-colors";

function Card({
  label,
  icon: Icon,
  tone = "ok",
  fleetWide,
  footer,
  children,
}: {
  label: string;
  icon: LucideIcon;
  tone?: Tone;
  fleetWide?: boolean;
  footer: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <>
      <div className="flex items-center justify-between text-xs text-fg-secondary">
        <span className="flex items-center gap-2">
          {label}
          {fleetWide && (
            // The backend did not apply the selected scope to this metric.
            <span
              title="Not filtered by the selected infrastructure scope"
              className="px-1.5 py-0.5 rounded bg-info-bg text-info-fg border border-info-line text-[10px] font-medium"
            >
              Fleet-wide
            </span>
          )}
        </span>
        <div className={`w-6 h-6 rounded border flex items-center justify-center ${TILE[tone]}`}>
          <Icon className="w-3.5 h-3.5" aria-hidden />
        </div>
      </div>
      <div className="my-2.5">{children}</div>
      <div className="pt-2.5 border-t border-app-border-subtle text-[11px] text-fg-muted">{footer}</div>
    </>
  );
}

const VALUE = "text-[24px] font-semibold tracking-tight font-num flex items-baseline gap-1.5";

export function MetricsRow() {
  const { scope, range, isScoped, carry } = useScope();
  const { data, isPending, isError, error } = useDashboardSummary(summaryParamsOf(scope, range));

  if (isPending) {
    return (
      <div aria-label="Loading summary" className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="h-36 rounded-btn bg-app-surface border border-app-border animate-pulse" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div role="alert" className="rounded-btn bg-app-surface border border-app-border p-4 shadow-card">
        <p className="text-sm font-medium text-fg">Couldn&apos;t load summary metrics</p>
        <p className="text-xs text-fg-muted mt-1">
          {error instanceof Error ? error.message : "The summary endpoint is unavailable."}
        </p>
      </div>
    );
  }

  const windowPhrase = WINDOWS.find((w) => w.value === data.window)?.phrase ?? "previous period";
  const fleetWide = (key: string) => isScoped && data.unscoped_metrics.includes(key);
  const projected = formatMoney(data.projected_savings);
  const verified = formatMoney(data.verified_operational_savings);
  const rollbackPct = data.guarded_changes > 0 ? (data.rollbacks / data.guarded_changes) * 100 : null;
  const ratio = data.projected_change_ratio;

  return (
    <div className="space-y-3">
      {data.freshness !== "fresh" && (
        // PRD §17.4: say so when the numbers may be out of date.
        <div role="status" className="rounded-btn border border-warn-line bg-warn-bg px-3 py-2 text-xs text-warn-fg">
          These figures may be out of date. Last generated {relativeTime(data.generated_at)}.
        </div>
      )}
      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3">
        <div className={CARD}>
          <Card
            label="Projected monthly savings"
            icon={TrendingUp}
            footer={`${data.open_recommendations} open recommendations ${isScoped ? "in this scope" : "across fleet"}`}
          >
            <div className={`${VALUE} text-fg`}>
              <span>{projected.display}</span>
              <span className="text-xs font-normal text-fg-muted">/ mo</span>
            </div>
            <div className="flex items-center gap-1.5 mt-1 text-xs">
              {ratio === null ? (
                <span className="text-fg-muted">No period comparison available</span>
              ) : (
                <>
                  <span className="text-ok-strong font-medium font-num">
                    {ratio >= 0 ? "+" : ""}
                    {(ratio * 100).toFixed(1)}%
                  </span>
                  <span className="text-fg-muted">vs. {windowPhrase}</span>
                </>
              )}
            </div>
          </Card>
        </div>

        <div className={CARD}>
          <Card
            label="Verified monthly savings"
            icon={CheckCircle2}
            fleetWide={fleetWide("verified_operational_savings")}
            footer="Operationally verified after post-action checks. Not billing-confirmed."
          >
            <div className={`${VALUE} text-fg`}>
              <span>{verified.display}</span>
              <span className="text-xs font-normal text-fg-muted">/ mo</span>
            </div>
            <div className="mt-1 text-xs text-ok-strong font-medium font-num">
              {data.verified_changes} verified changes
            </div>
          </Card>
        </div>

        {/* Clicking filters the queue to approvals (PRD §7.3). */}
        <Link
          href={carry("/?attention=approvals")}
          className={`${CARD} focus-visible:outline focus-visible:outline-2 focus-visible:outline-emerald-500`}
        >
          <Card
            label="Pending approvals"
            icon={AlertCircle}
            tone="warn"
            footer={`${data.pending_approvals_mine} assigned directly to you`}
          >
            <div className="flex items-baseline gap-2">
              <span className="text-[24px] font-semibold tracking-tight text-fg font-num">{data.pending_approvals}</span>
              <span className="text-xs text-fg-secondary">actions</span>
              <span className="text-xs font-medium text-warn-fg font-num">
                ({formatMoneyCompact(data.pending_approvals_savings)}/mo)
              </span>
            </div>
            <div className="mt-1">
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-warn-bg text-warn-fg border border-warn-line">
                Requires approval
              </span>
            </div>
          </Card>
        </Link>

        <div className={CARD}>
          <Card
            label="Rollback rate"
            icon={RefreshCw}
            fleetWide={fleetWide("rollback_rate")}
            footer={
              rollbackPct === null
                ? "No guarded changes in this window"
                : `${data.rollbacks} of ${data.guarded_changes} guarded changes rolled back`
            }
          >
            <div className={`${VALUE} ${rollbackPct && rollbackPct > 0 ? "text-warn-fg" : "text-ok-strong"}`}>
              {rollbackPct === null ? "—" : `${rollbackPct.toFixed(1)}%`}
            </div>
            <div className="mt-1 text-xs text-ok-strong font-medium font-num">{data.rollbacks} rollbacks</div>
          </Card>
        </div>
      </div>
    </div>
  );
}