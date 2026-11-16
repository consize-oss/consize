"use client";

import { PoliciesHeader } from "./PoliciesHeader";
import { PoliciesSummary } from "./PoliciesSummary";
import { usePolicyInventory, PolicyInventoryProvider } from "./policyInventory";

// Same composition as the Resources screen; one provider owns the data.
export function PoliciesScreen() {
  return (
    <PolicyInventoryProvider>
      <PoliciesHeader />
      <PoliciesSummary />
      <RuntimePolicy />
    </PolicyInventoryProvider>
  );
}
function RuntimePolicy() {
  const { runtimePolicy, isLoading, isError, retry } = usePolicyInventory();
  if (isLoading) return <p role="status">Loading configured policy…</p>;
  if (isError)
    return (
      <div
        role="alert"
        className="rounded-card border border-app-border bg-app-surface p-5"
      >
        <p>Couldn&apos;t load the configured policy.</p>
        <button
          className="mt-3 rounded-btn border border-app-border p-2"
          onClick={() => void retry()}
        >
          Retry
        </button>
      </div>
    );
  return (
    <section className="rounded-card border border-app-border bg-app-surface p-5 space-y-3">
      <h2 className="font-semibold text-fg">{runtimePolicy?.name}</h2>
      <dl className="grid gap-3 text-sm text-fg-secondary sm:grid-cols-2">
        {Object.entries({
          "Policy ID": runtimePolicy?.id,
          Mode: runtimePolicy?.mode,
          Approval: runtimePolicy?.approval_model,
          Verification: runtimePolicy?.verification,
        }).map(([label, value]) => (
          <div key={label}>
            <dt className="text-xs text-fg-muted">{label}</dt>
            <dd className="mt-1 break-words">{value || "Unavailable"}</dd>
          </div>
        ))}
      </dl>
      <p className="text-xs text-fg-muted">
        The runtime supplies this foundation policy. Editable policy inventory,
        coverage, exception counts and historical totals are not available in
        this release.
      </p>
    </section>
  );
}
