"use client";

import { PoliciesAttentionStrip } from "./PoliciesAttentionStrip";
import { PoliciesHeader } from "./PoliciesHeader";
import { PoliciesInventoryPanel } from "./PoliciesInventoryPanel";
import { PoliciesSummary } from "./PoliciesSummary";
import { PolicyInventoryProvider } from "./policyInventory";

// Same composition as the Resources screen; one provider owns the data.
export function PoliciesScreen() {
  return (
    <PolicyInventoryProvider>
      <PoliciesHeader />
      <PoliciesSummary />
      <PoliciesAttentionStrip />
      <PoliciesInventoryPanel />
    </PolicyInventoryProvider>
  );
}