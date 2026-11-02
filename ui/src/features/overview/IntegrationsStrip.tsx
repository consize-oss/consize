// "use client";

// import Link from "next/link";
// import { relativeTime } from "@/lib/format";
// import type { Capability } from "@/schemas";
// import { useCapabilities, useDashboardSummary } from "./queries";

// const HEALTH: Record<Capability["health"], { label: string; pill: string }> = {
//   healthy: { label: "Healthy", pill: "bg-ok-bg text-ok-fg border-ok-line" },
//   degraded: { label: "Degraded", pill: "bg-warn-bg text-warn-fg border-warn-line" },
//   unavailable: { label: "Unavailable", pill: "bg-bad-bg text-bad-fg border-bad-line" },
//   setup_required: { label: "Setup required", pill: "bg-app-surface-alt text-fg-secondary border-app-border" },
// };

// const VERIFIER_DOT = { healthy: "bg-emerald-400", degraded: "bg-amber-400", unavailable: "bg-rose-400" } as const;
// const VERIFIER_LABEL = { healthy: "healthy", degraded: "degraded", unavailable: "unavailable" } as const;

// export function IntegrationsStrip() {
//   const { data, isPending, isError, error } = useCapabilities();
//   // Verifier health comes from the (unscoped) summary, shared with the sidebar.
//   const { data: summary } = useDashboardSummary({});

//   if (isPending) {
//     return <div aria-label="Loading integrations" className="h-24 rounded-btn bg-app-surface border border-app-border animate-pulse" />;
//   }

//   if (isError) {
//     return (
//       <div role="alert" className="rounded-btn bg-app-surface border border-app-border px-4 py-3 shadow-card">
//         <p className="text-sm font-medium text-fg">Couldn&apos;t load integration health</p>
//         <p className="text-xs text-fg-muted mt-1">
//           {error instanceof Error ? error.message : "The integrations endpoint is unavailable."}
//         </p>
//       </div>
//     );
//   }

//   const needAttention = data.items.filter((i) => i.health !== "healthy").length;

//   return (
//     <div className="bg-app-surface border border-app-border rounded-btn px-4 py-3 space-y-3 shadow-card">
//       <div className="flex items-center justify-between text-xs">
//         <div className="flex items-center gap-2">
//           <span className="font-medium text-fg">Integration health</span>
//           <span className="text-fg-muted" aria-hidden>•</span>
//           <span className="text-fg-secondary">
//             {data.items.length} connected •{" "}
//             {needAttention === 0 ? "no action required" : `${needAttention} need attention`}
//           </span>
//           {summary && (
//             <>
//               <span className="hidden lg:inline text-fg-faint" aria-hidden>|</span>
//               <span className="hidden lg:inline-flex items-center gap-1.5 text-fg-muted">
//                 <span className={`h-1.5 w-1.5 rounded-full ${VERIFIER_DOT[summary.verifier]}`} aria-hidden />
//                 <span>Verification {VERIFIER_LABEL[summary.verifier]}</span>
//               </span>
//             </>
//           )}
//         </div>
//         <Link href="/plugins" className="text-xs font-medium text-ok-strong hover:underline flex items-center gap-1">
//           <span>View all integrations</span>
//           <span aria-hidden>→</span>
//         </Link>
//       </div>

//       {data.items.length === 0 ? (
//         <p className="text-xs text-fg-muted">
//           No integrations are connected yet. Connect Kubernetes and Prometheus to start collecting evidence.
//         </p>
//       ) : (
//         <div className="hidden xl:grid grid-cols-5 gap-2">
//           {data.items.map((item) => {
//             const h = HEALTH[item.health];
//             return (
//               <Link
//                 key={item.id}
//                 href="/plugins"
//                 title={item.message}
//                 className="bg-app-subcard border border-app-border rounded-btn p-2.5 flex items-center justify-between gap-2 hover:border-app-border-strong transition-colors"
//               >
//                 <div className="min-w-0">
//                   <div className="text-xs font-medium text-fg truncate">{item.display_name}</div>
//                   <div className="text-[11px] text-fg-muted truncate">
//                     {item.capability} • checked {relativeTime(item.last_checked_at)}
//                   </div>
//                 </div>
//                 <span className={`px-2 py-0.5 rounded border text-[10px] font-medium shrink-0 ${h.pill}`}>{h.label}</span>
//               </Link>
//             );
//           })}
//         </div>
//       )}
//     </div>
//   );
// }
"use client";

import Link from "next/link";
import { relativeTime } from "@/lib/format";
import { PILL, SEVERITY, pluginStatus } from "../plugins/status";
import { usePlugins } from "../plugins/queries";
import { useDashboardSummary } from "./queries";

const VERIFIER_DOT = { healthy: "bg-emerald-400", degraded: "bg-amber-400", unavailable: "bg-rose-400" } as const;
const MAX_SHOWN = 5;

export function IntegrationsStrip() {
  // Same endpoint and same status rules as the Plugins screen, so the two
  // views cannot disagree about a plugin's health.
  const { data, isPending, isError, error } = usePlugins({});
  // Verifier health comes from the (unscoped) summary, shared with the sidebar.
  const { data: summary } = useDashboardSummary({});

  if (isPending) {
    return <div aria-label="Loading integrations" className="h-24 rounded-btn bg-app-surface border border-app-border animate-pulse" />;
  }

  if (isError) {
    return (
      <div role="alert" className="rounded-btn bg-app-surface border border-app-border px-4 py-3 shadow-card">
        <p className="text-sm font-medium text-fg">Couldn&apos;t load integration health</p>
        <p className="text-xs text-fg-muted mt-1">
          {error instanceof Error ? error.message : "The integrations endpoint is unavailable."}
        </p>
      </div>
    );
  }

  const withStatus = data.items.map((p) => ({ p, status: pluginStatus(p) }));
  const needAttention = withStatus.filter((x) => x.status.tone !== "ok").length;
  // Problems first: the strip is for noticing what needs attention.
  const shown = [...withStatus]
    .sort((a, b) => SEVERITY[a.status.tone] - SEVERITY[b.status.tone])
    .slice(0, MAX_SHOWN);

  return (
    <div className="bg-app-surface border border-app-border rounded-btn px-4 py-3 space-y-3 shadow-card">
      <div className="flex items-center justify-between text-xs">
        <div className="flex items-center gap-2">
          <span className="font-medium text-fg">Integration health</span>
          <span className="text-fg-muted" aria-hidden>•</span>
          <span className="text-fg-secondary">
            {data.items.length} connected •{" "}
            {needAttention === 0 ? "no action required" : `${needAttention} need attention`}
          </span>
          {summary && (
            <>
              <span className="hidden lg:inline text-fg-faint" aria-hidden>|</span>
              <span className="hidden lg:inline-flex items-center gap-1.5 text-fg-muted">
                <span className={`h-1.5 w-1.5 rounded-full ${VERIFIER_DOT[summary.verifier]}`} aria-hidden />
                <span>Verification {summary.verifier}</span>
              </span>
            </>
          )}
        </div>
        <Link href="/plugins" className="text-xs font-medium text-ok-strong hover:underline flex items-center gap-1">
          <span>View all integrations</span>
          <span aria-hidden>→</span>
        </Link>
      </div>

      {data.items.length === 0 ? (
        <p className="text-xs text-fg-muted">
          No integrations are connected yet. Connect Kubernetes and Prometheus to start collecting evidence.
        </p>
      ) : (
        <div className="hidden xl:grid grid-cols-5 gap-2">
          {shown.map(({ p, status }) => (
            <Link
              key={p.id}
              href={`/plugins?plugin=${encodeURIComponent(p.id)}`}
              title={p.message}
              className="bg-app-subcard border border-app-border rounded-btn p-2.5 flex items-center justify-between gap-2 hover:border-app-border-strong transition-colors"
            >
              <div className="min-w-0">
                <div className="text-xs font-medium text-fg truncate">{p.display_name}</div>
                <div className="text-[11px] text-fg-muted truncate">
                  {p.capability} • checked {relativeTime(p.last_checked_at)}
                </div>
              </div>
              <span className={`px-2 py-0.5 rounded border text-[10px] font-medium shrink-0 ${PILL[status.tone]}`}>
                {status.label}
              </span>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}