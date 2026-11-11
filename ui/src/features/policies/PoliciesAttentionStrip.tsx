"use client";

import { AttentionStrip, type AttentionItem } from "@/components/ui/AttentionStrip";
import { usePolicyInventory } from "./policyInventory";

const plural = (n: number, one: string, many: string) => (n === 1 ? one : many);

export function PoliciesAttentionStrip() {
  const { summary, isLoading, isError, carry } = usePolicyInventory();
  const s = summary;
  const uncovered = s ? Math.max(0, s.coverage.total - s.coverage.covered) : null;

  const items: AttentionItem[] = [
    { id: "uncovered", count: uncovered, dotClass: "bg-amber-400", label: plural(uncovered ?? 0, "resource uncovered", "resources uncovered") },
    { id: "expiring", count: s?.exceptions.expiring_soon ?? null, dotClass: "bg-amber-400", label: plural(s?.exceptions.expiring_soon ?? 0, "exception expiring", "exceptions expiring") },
    { id: "draft", count: s?.drafts ?? null, dotClass: "bg-sky-400", label: plural(s?.drafts ?? 0, "policy in draft", "policies in draft") },
    { id: "conflicts", count: s?.conflicts ?? null, dotClass: "bg-emerald-400", label: plural(s?.conflicts ?? 0, "policy conflict", "policy conflicts") },
  ];

  return (
    <AttentionStrip
      title="Governance requiring attention"
      ariaLabel="Governance requiring attention"
      items={items}
      loading={isLoading}
      unavailable={isError}
      // Hand off to the Resources screen, already filtered to what's uncovered.
      action={{ label: "Review coverage", href: carry("/resources?coverage=uncovered") }}
    />
  );
}