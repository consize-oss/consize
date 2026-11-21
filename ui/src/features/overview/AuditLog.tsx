"use client";

import { AlertTriangle, Ban, Check, CheckCheck, ClipboardList, Clock, GitPullRequest, RotateCcw } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { relativeTime } from "@/lib/format";
import type { AuditEvent } from "@/schemas";
import { useAuditEvents } from "./queries";

type Tone = "ok" | "info" | "warn" | "bad";
const TONE: Record<Tone, { tile: string; text: string }> = {
  ok: { tile: "bg-ok-bg border-ok-line text-ok-strong", text: "text-ok-strong" },
  info: { tile: "bg-info-bg border-info-line text-info-fg", text: "text-info-fg" },
  warn: { tile: "bg-warn-bg border-warn-line text-warn-fg", text: "text-warn-fg" },
  bad: { tile: "bg-bad-bg border-bad-line text-bad-fg", text: "text-bad-fg" },
};

const OUTCOME: Record<AuditEvent["outcome"], { icon: LucideIcon; tone: Tone }> = {
  pending: { icon: Clock, tone: "info" },
  applied: { icon: Check, tone: "ok" },
  verified: { icon: CheckCheck, tone: "ok" },
  verifying: { icon: Clock, tone: "info" },
  awaiting_approval: { icon: GitPullRequest, tone: "info" },
  rolled_back: { icon: RotateCcw, tone: "warn" },
  manual_intervention: { icon: AlertTriangle, tone: "warn" },
  blocked: { icon: Ban, tone: "bad" },
  failed: { icon: AlertTriangle, tone: "bad" },
};

const CHIP = "font-mono text-fg-secondary bg-app-subcard px-1.5 py-0.5 rounded border border-app-border text-[10px]";

// Emphasise the resource name inside the sentence the backend wrote.
function Title({ event, tone }: { event: AuditEvent; tone: Tone }) {
  const { title, highlight } = event;
  const i = highlight ? title.indexOf(highlight) : -1;
  if (!highlight || i < 0) return <>{title}</>;
  return (
    <>
      {title.slice(0, i)}
      <span className={`font-mono ${TONE[tone].text}`}>{highlight}</span>
      {title.slice(i + highlight.length)}
    </>
  );
}

export function AuditLog() {
  const { data, isPending, isError, error } = useAuditEvents();

  return (
    <div className="bg-app-surface border border-app-border rounded-card overflow-hidden shadow-card">
      <div className="px-5 py-3.5 border-b border-app-border flex items-center gap-2.5 bg-app-toolbar">
        <ClipboardList className="w-4 h-4 text-fg-muted" aria-hidden />
        <h2 className="text-sm font-semibold text-fg">Verification &amp; audit activity</h2>
      </div>

      {isPending ? (
        <div aria-label="Loading audit activity" className="p-4 space-y-3 animate-pulse">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="h-12 bg-app-surface-alt rounded" />
          ))}
        </div>
      ) : isError ? (
        <div role="alert" className="p-6">
          <p className="text-sm font-medium text-fg">Couldn&apos;t load audit activity</p>
          <p className="text-xs text-fg-muted mt-1">
            {error instanceof Error ? error.message : "The audit endpoint is unavailable."}
          </p>
        </div>
      ) : data.items.length === 0 ? (
        <div className="px-5 py-10 text-center">
          <p className="text-sm font-medium text-fg">No applied changes yet</p>
          <p className="text-xs text-fg-muted mt-1">Approved actions and their verification results appear here.</p>
        </div>
      ) : (
        <ol className="divide-y divide-app-border">
          {data.items.map((event) => {
            const { icon: Icon, tone } = OUTCOME[event.outcome];
            const blocked = event.outcome === "blocked";
            const parts: React.ReactNode[] = [];
            if (event.signal) {
              parts.push(<span key="s" className={`${blocked ? "" : `${TONE[tone].text} font-medium`} font-num`}>{event.signal}</span>);
            }
            if (event.branch) {
              parts.push(<span key="b">Branch: <code className={CHIP}>{event.branch}</code></span>);
            }
            parts.push(<span key="a">Actor: <code className={CHIP}>{event.actor}</code></span>);
            if (event.policy_id) {
              parts.push(<span key="p">{blocked ? "Guard" : "Rule"}: <code className="font-mono text-[10px] text-fg-secondary">{event.policy_id}</code></span>);
            }
            if (event.note) {
              parts.push(<span key="n" className={event.outcome === "awaiting_approval" ? "text-warn-fg font-medium" : ""}>{event.note}</span>);
            }
            return (
              <li key={event.id} className="p-4 flex items-start gap-3.5 hover:bg-app-hover transition-colors">
                <div className={`w-7 h-7 rounded-btn border flex items-center justify-center shrink-0 mt-0.5 ${TONE[tone].tile}`}>
                  <Icon className="w-3.5 h-3.5" aria-hidden />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex flex-col sm:flex-row sm:items-baseline justify-between gap-1">
                    <span className={`font-medium text-xs ${blocked ? "text-bad-fg" : "text-fg"}`}>
                      <Title event={event} tone={tone} />
                    </span>
                    <time
                      dateTime={event.occurred_at}
                      title={new Date(event.occurred_at).toLocaleString()}
                      className="text-[11px] text-fg-muted shrink-0"
                    >
                      {relativeTime(event.occurred_at)}
                    </time>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 mt-1 text-xs text-fg-muted">
                    {parts.map((p, i) => (
                      <span key={i} className="inline-flex items-center gap-2">
                        {i > 0 && <span aria-hidden>•</span>}
                        {p}
                      </span>
                    ))}
                  </div>
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}