"use client";

import { useSyncExternalStore } from "react";
import { AuditLog } from "./AuditLog";
import { IntegrationsStrip } from "./IntegrationsStrip";
import { MetricsRow } from "./MetricsRow";
import { OverviewHeader } from "./OverviewHeader";
import { RecommendationPanel } from "./RecommendationPanel";

// React uses the server snapshot again during hydration, even if the sidebar
// has already populated the shared query cache. Normal client navigation is ready immediately.
const subscribe = () => () => {};
const getClientSnapshot = () => true;
const getServerSnapshot = () => false;

export function OverviewContent() {
  const hydrated = useSyncExternalStore(subscribe, getClientSnapshot, getServerSnapshot);

  // Keep the first server/browser render identical before mounting query-driven panels.
  if (!hydrated) {
    return (
      <div role="status" aria-label="Loading overview" className="h-96 rounded-btn bg-app-surface border border-app-border animate-pulse">
        <span className="sr-only">Loading overview...</span>
      </div>
    );
  }

  return (
    <>
      <OverviewHeader />
      {!(process.env.NODE_ENV === "development" && process.env.NEXT_PUBLIC_USE_MOCKS === "true") && <p className="text-xs text-fg-muted">Current inventory and recorded workflow history. Date ranges and monetary totals are not available from this runtime.</p>}
      <MetricsRow />
      <IntegrationsStrip />
      <RecommendationPanel />
      <AuditLog />
    </>
  );
}