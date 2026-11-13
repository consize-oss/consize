"use client";

import Link from "next/link";
import { CheckCircle2, Download, RefreshCw } from "lucide-react";
import { relativeTime } from "@/lib/format";
import { summaryParamsOf, useScope } from "../scope/useScope";
import { useDashboardSummary } from "./queries";

const DISABLED =
  "opacity-60 cursor-not-allowed disabled:pointer-events-none";

export function OverviewHeader() {
  const { scope, range, carry } = useScope();
  const { data } = useDashboardSummary(summaryParamsOf(scope, range));

  return (
    <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4 pb-4 border-b border-app-border">
      <div className="space-y-1">
        <div className="flex items-center gap-3">
          <h1 className="text-[24px] font-semibold tracking-tight text-fg">Overview</h1>
          {/* PRD §7.2: never claim enforcement the backend hasn't reported. */}
          {data?.policy_engine === "enforcing" && (
            <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full bg-ok-bg text-ok-fg border border-ok-line text-xs font-medium">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" aria-hidden />
              Policy enforcement on
            </span>
          )}
          {data?.policy_engine === "unavailable" && (
            <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full bg-warn-bg text-warn-fg border border-warn-line text-xs font-medium">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-400" aria-hidden />
              Policy engine unavailable
            </span>
          )}
        </div>
        <p className="text-xs text-fg-secondary">
          Discover savings, evaluate policies, and verify infrastructure changes.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2.5 shrink-0">
        <div className="flex items-center gap-1.5 pr-1 text-[11px] text-fg-muted">
          {data?.last_evaluated_at && (
            <span title={new Date(data.last_evaluated_at).toLocaleString()}>
              Last evaluated {relativeTime(data.last_evaluated_at)}
            </span>
          )}
          {/* Evaluation is a command the backend doesn't expose yet. */}
          <button
            type="button"
            disabled
            aria-label="Run evaluation now (not available yet)"
            title="Not available yet"
            className={`w-8 h-8 flex items-center justify-center border border-app-border rounded-btn text-fg-secondary ${DISABLED}`}
          >
            <RefreshCw className="w-3.5 h-3.5" aria-hidden />
          </button>
        </div>
        <button
          type="button"
          disabled
          title="Not available yet"
          className={`h-9 px-3.5 bg-app-surface text-fg border border-app-border rounded-btn text-xs font-medium flex items-center gap-2 ${DISABLED}`}
        >
          <Download className="w-4 h-4 text-fg-muted" aria-hidden />
          <span>Export summary</span>
        </button>
        <Link
          href={carry("/approvals")}
          className="h-9 px-4 bg-emerald-700 hover:bg-emerald-800 text-white rounded-btn text-xs font-medium flex items-center gap-2 shadow-sm transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-500"
        >
          <CheckCircle2 className="w-4 h-4" aria-hidden />
          <span>Review approvals</span>
          {data && (data.pending_approvals_mine ?? 0) > 0 && (
            <span className="px-1.5 py-0.5 rounded bg-white/15 text-[10px]">{data.pending_approvals_mine}</span>
          )}
        </Link>
      </div>
    </div>
  );
}