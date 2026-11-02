"use client";

import { CheckCircle2 } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { relativeTimeShort } from "@/lib/format";
import type { PluginDetail } from "@/schemas";
import { usePluginDetail } from "./queries";
import { DOT, FACT_TEXT, KIND, PILL, pluginStatus } from "./status";

const SHELL = "overflow-hidden rounded-card border border-app-border bg-app-surface shadow-card";
const H3 = "text-[10px] font-semibold uppercase tracking-wider text-fg-muted";
const BOX = "rounded-btn border border-app-border bg-app-subcard p-3";
const DISABLED_BTN =
  "grid h-8 w-full place-items-center rounded-btn border border-app-border text-xs font-medium text-fg-secondary opacity-60 cursor-not-allowed";

const CRED = {
  encrypted: { label: "Encrypted", cls: "text-ok-strong" },
  expiring: { label: "Expiring soon", cls: "text-warn-fg" },
  invalid: { label: "Rejected by provider", cls: "text-bad-fg" },
} as const;

// "Online · checked 32s ago" / "Connection failing · HTTP 401": one honest
// line instead of an always-green "synchronized".
function statusLine(p: PluginDetail) {
  const s = pluginStatus(p);
  if (p.health === "unavailable") return `${s.label}${p.status_reason ? ` · ${p.status_reason}` : ""}`;
  if (p.health === "healthy" && s.tone === "ok") return `Online · checked ${relativeTimeShort(p.last_checked_at)}`;
  return `${s.label} · checked ${relativeTimeShort(p.last_checked_at)}`;
}

export function PluginPanel({ id }: { id: string | undefined }) {
  const { data: p, isPending, isError, error } = usePluginDetail(id);

  if (!id) {
    return (
      <aside className={`${SHELL} p-5`}>
        <p className="text-sm font-medium text-fg">No plugin selected</p>
        <p className="mt-1 text-xs text-fg-muted">Select a plugin to inspect its scope, guardrails, and credentials.</p>
      </aside>
    );
  }
  if (isPending) {
    return <aside aria-label="Loading plugin" className={`${SHELL} h-80 animate-pulse`} />;
  }
  if (isError) {
    const gone = error instanceof ApiError && error.status === 404;
    return (
      <aside role="alert" className={`${SHELL} p-5`}>
        <p className="text-sm font-medium text-fg">{gone ? "This plugin is no longer installed" : "Couldn't load this plugin"}</p>
        <p className="mt-1 text-xs text-fg-muted">
          {gone ? "It may have been removed since you opened it." : error instanceof Error ? error.message : "The plugin endpoint is unavailable."}
        </p>
      </aside>
    );
  }

  const status = pluginStatus(p);
  const kind = KIND[p.kind];
  const Icon = kind.icon;

  return (
    <aside aria-label={`${p.display_name} details`} className={SHELL}>
      <div className="border-b border-app-border p-4">
        <div className="flex items-start gap-3">
          <span className={`grid h-9 w-9 shrink-0 place-items-center rounded-btn border ${kind.tile}`}>
            <Icon className="h-5 w-5" aria-hidden />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex items-center justify-between gap-2">
              <h2 className="truncate text-sm font-semibold text-fg">{p.display_name}</h2>
              <span className={`rounded border px-2 py-0.5 text-[10px] font-semibold ${kind.chip}`}>{kind.label}</span>
            </div>
            <div className={`mt-1 flex items-center gap-1.5 text-[11px] ${status.tone === "ok" ? "text-ok-strong" : status.tone === "bad" ? "text-bad-fg" : status.tone === "warn" ? "text-warn-fg" : "text-fg-secondary"}`}>
              <i className={`h-1.5 w-1.5 rounded-full ${DOT[status.tone]}`} aria-hidden />
              {statusLine(p)}
            </div>
          </div>
        </div>
        {p.message && p.health !== "healthy" && (
          <p className={`mt-3 rounded-btn border px-3 py-2 text-xs ${PILL[status.tone]}`}>{p.message}</p>
        )}
      </div>

      <div className="space-y-4 p-4">
        <section>
          <h3 className={H3}>Overview</h3>
          <p className="mt-2 text-xs leading-5 text-fg-secondary">{p.overview}</p>
        </section>

        <section>
          <h3 className={H3}>Connected scope</h3>
          <dl className={`mt-2 grid grid-cols-2 gap-2 ${BOX}`}>
            {p.scope_stats.map((s) => (
              <div key={s.label} className="min-w-0">
                <dt className="text-[10px] text-fg-muted">{s.label}</dt>
                <dd className={`mt-1 text-xs font-semibold ${s.tone === "default" ? "text-fg" : FACT_TEXT[s.tone]}`}>{s.value}</dd>
                {s.sub && <dd className="text-[10px] text-fg-secondary">{s.sub}</dd>}
              </div>
            ))}
          </dl>
        </section>

        {p.guardrail && (
          <section>
            <h3 className={H3}>Verification guardrail</h3>
            <div className={`mt-2 ${BOX}`}>
              <div className="flex items-center gap-2 text-xs font-semibold text-ok-strong">
                <CheckCircle2 className="h-4 w-4" aria-hidden />
                {p.guardrail.title}
              </div>
              <p className="mt-2 text-[11px] leading-5 text-fg-secondary">{p.guardrail.description}</p>
            </div>
          </section>
        )}

        <section>
          <div className="flex items-center justify-between">
            <h3 className={H3}>Credentials</h3>
            {p.credentials && (
              <span className={`text-[10px] ${CRED[p.credentials.state].cls}`}>{CRED[p.credentials.state].label}</span>
            )}
          </div>
          <div className={`mt-2 text-[11px] text-fg-secondary ${BOX}`}>
            {p.credentials ? (
              p.credentials.entries.map((e, i) => (
                <div key={e.label} className={`flex justify-between ${i > 0 ? "mt-2" : ""}`}>
                  <span>{e.label}</span>
                  {/* Values are masked server-side; the browser never receives secrets. */}
                  <span className="font-mono">{e.masked}</span>
                </div>
              ))
            ) : (
              <span>No credentials required.</span>
            )}
          </div>
        </section>

        {/* None of these commands or routes exist yet, so they stay visibly
            disabled rather than looking live and doing nothing. */}
        <div className="grid grid-cols-1 gap-2">
          <button type="button" disabled title="Not available yet" className={DISABLED_BTN}>Test connection</button>
          <button type="button" disabled title="Not available yet" className={DISABLED_BTN}>Configure plugin</button>
          <span className="text-center text-xs text-fg-muted" title="Not available yet">View plugin logs (not available yet)</span>
        </div>
      </div>
    </aside>
  );
}