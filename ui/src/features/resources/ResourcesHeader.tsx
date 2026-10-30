"use client";

import { useState } from "react";
import { Download, RefreshCw } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { relativeTime } from "@/lib/format";
import { useResourceInventory } from "./resourceInventory";

const ACTION_BUTTON = "inline-flex h-8 cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-btn border px-3 text-xs font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed disabled:opacity-60";
type Notice = { error: boolean; text: string } | null;

export function ResourcesHeader() {
  const { updatedAt, asOf, filteredItems, isLoading, isError, isRetrying, isSyncing, syncError, refresh, syncInventory, exportInventory } = useResourceInventory();
  const [notice, setNotice] = useState<Notice>(null);
  const busy = isRetrying || isSyncing;
  const errorText = syncError instanceof ApiError && syncError.status === 403
    ? "You do not have permission to sync inventory."
    : syncError instanceof ApiError && syncError.status === 401
      ? "Inventory sync requires authentication."
      : syncError ? "Could not sync inventory. Please try again."
        : notice?.error ? notice.text : isError ? "Inventory is unavailable. Try refreshing." : null;

  return (
    <header className="flex flex-col gap-3 border-b border-app-border pb-3 lg:flex-row lg:items-center lg:justify-between">
      <div className="min-w-0 flex-1 space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight text-fg">Resources</h1>
        <p className="text-xs leading-4 text-fg-secondary">
          Inventory, ownership, observability, and governance coverage across connected infrastructure.
        </p>
      </div>

      <div className="space-y-2 lg:shrink-0">
        <div className="flex flex-wrap items-center gap-2">
          <p className="whitespace-nowrap text-[11px] text-fg-muted">
            Inventory updated{" "}
            {!isLoading && !isError && updatedAt
              ? <time dateTime={updatedAt} title={updatedAt}>{relativeTime(updatedAt, asOf)}</time>
              : <span title="Inventory update time is unavailable">{"\u2014"}</span>}
          </p>
          <button type="button" disabled={busy} aria-label="Refresh inventory" aria-busy={isRetrying}
            title="Reload the current inventory"
            onClick={async () => {
              setNotice(null);
              const success = await refresh();
              setNotice({ error: !success, text: success ? "Inventory refreshed." : "Could not refresh inventory. Please try again." });
            }}
            className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-btn border border-app-border bg-app-surface text-fg-secondary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <RefreshCw aria-hidden="true" className={"h-3.5 w-3.5 " + (isRetrying ? "animate-spin motion-reduce:animate-none" : "")} />
          </button>
          <button type="button" disabled={isLoading || isError || busy || filteredItems.length === 0}
            title={"Export all " + filteredItems.length + " matching resources as CSV"}
            onClick={() => {
              try {
                const count = exportInventory();
                setNotice({ error: false, text: "Exported " + count + " resource" + (count === 1 ? "." : "s.") });
              } catch {
                setNotice({ error: true, text: "Could not export inventory. Please try again." });
              }
            }}
            className={ACTION_BUTTON + " border-app-border bg-app-surface text-fg"}
          >
            <Download aria-hidden="true" className="h-3.5 w-3.5 text-fg-muted" />
            Export inventory
          </button>
          <button type="button" disabled={busy} aria-busy={isSyncing}
            title="Sync inventory from all configured discovery sources"
            onClick={async () => {
              setNotice(null);
              const success = await syncInventory();
              if (success) setNotice({ error: false, text: "Inventory synced." });
            }}
            className={ACTION_BUTTON + " border-emerald-600 bg-emerald-600 text-white"}
          >
            <RefreshCw aria-hidden="true" className={"h-3.5 w-3.5 " + (isSyncing ? "animate-spin motion-reduce:animate-none" : "")} />
            {isSyncing ? "Syncing..." : "Sync inventory"}
          </button>
        </div>
        {errorText
          ? <p role="alert" className="text-xs text-bad-fg lg:text-right">{errorText}</p>
          : notice && <p role="status" className="text-xs text-ok-strong lg:text-right">{notice.text}</p>}
      </div>
    </header>
  );
}
