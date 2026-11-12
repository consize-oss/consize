"use client";

import { useState } from "react";
import { Download, FlaskConical, Plus, RefreshCw } from "lucide-react";
import { HEADER_ACTION, HEADER_ICON_BUTTON, PageHeader } from "@/components/ui/PageHeader";
import { relativeTime } from "@/lib/format";
import { usePolicyInventory } from "./policyInventory";

type Notice = { error: boolean; text: string } | null;

export function PoliciesHeader() {
  const { summary, asOf, filteredItems, isLoading, isError, isRetrying, isScoped, refresh, exportMatrix } =
    usePolicyInventory();
  const [notice, setNotice] = useState<Notice>(null);
  const available = !isLoading && !isError && summary;
  const errorText = notice?.error ? notice.text : isError ? "Policies are unavailable. Try refreshing." : null;

  return (
    <PageHeader
      title="Policies"
      description={
        <>
          <p>Define where optimization is allowed, how changes are executed, and what must pass before they are accepted.</p>
          {isScoped && (
            // PRD §6.2: say so when a view can't be filtered by the active scope.
            <p className="mt-1 text-[11px] text-fg-muted">
              Policies apply workspace-wide, so the infrastructure scope doesn&apos;t filter this page.
            </p>
          )}
        </>
      }
    >
      <div className="flex flex-wrap items-center gap-2">
        <p className="whitespace-nowrap text-[11px] text-fg-muted">
          Evaluated{" "}
          {available ? (
            <time dateTime={summary.evaluated_at} title={summary.evaluated_at}>
              {relativeTime(summary.evaluated_at, asOf)}
            </time>
          ) : (
            <span title="Evaluation time is unavailable">{"\u2014"}</span>
          )}
        </p>
        <button
          type="button"
          disabled={isRetrying}
          aria-busy={isRetrying}
          aria-label="Refresh policies"
          title="Reload the current policies"
          onClick={async () => {
            setNotice(null);
            const ok = await refresh();
            setNotice({ error: !ok, text: ok ? "Policies refreshed." : "Could not refresh policies. Please try again." });
          }}
          className={HEADER_ICON_BUTTON}
        >
          <RefreshCw aria-hidden="true" className={"h-3.5 w-3.5 " + (isRetrying ? "animate-spin motion-reduce:animate-none" : "")} />
        </button>
        <button
          type="button"
          disabled={isLoading || isError || filteredItems.length === 0}
          title={"Export the " + filteredItems.length + " matching policies as CSV"}
          onClick={() => {
            try {
              const count = exportMatrix();
              setNotice({ error: false, text: "Exported " + count + " polic" + (count === 1 ? "y." : "ies.") });
            } catch {
              setNotice({ error: true, text: "Could not export the policy matrix. Please try again." });
            }
          }}
          className={HEADER_ACTION + " border-app-border bg-app-surface text-fg"}
        >
          <Download aria-hidden="true" className="h-3.5 w-3.5 text-fg-muted" />
          Export policy matrix
        </button>
        {/* Policy simulation and authoring are out of scope until the backend
            can evaluate a draft; these stay visibly disabled rather than fake. */}
        <button type="button" disabled title="Not available yet" className={HEADER_ACTION + " border-app-border bg-app-surface text-fg"}>
          <FlaskConical aria-hidden="true" className="h-3.5 w-3.5 text-fg-muted" />
          Simulate changes
        </button>
        <button type="button" disabled title="Not available yet" className={HEADER_ACTION + " border-app-border bg-app-surface text-fg"}>
          <Plus aria-hidden="true" className="h-3.5 w-3.5 text-fg-muted" />
          Create policy
        </button>
      </div>
      {errorText ? (
        <p role="alert" className="text-xs text-bad-fg lg:text-right">{errorText}</p>
      ) : (
        notice && <p role="status" className="text-xs text-ok-strong lg:text-right">{notice.text}</p>
      )}
    </PageHeader>
  );
}