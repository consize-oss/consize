"use client";

import { Ban, BarChart3, ShieldCheck, TriangleAlert } from "lucide-react";
import { SummaryCard } from "@/components/ui/SummaryCard";
import { usePolicyInventory } from "./policyInventory";

const plural = (n: number, one: string, many: string) => (n === 1 ? one : many);

export function PoliciesSummary() {
  const { summary, windowLabel, isLoading, isError } = usePolicyInventory();
  const s = !isLoading && !isError ? summary : undefined;
  const num = (v: number | undefined) => (v === undefined ? "\u2014" : v.toLocaleString("en-US"));
  const { covered = 0, total = 0 } = s?.coverage ?? {};

  return (
    <section aria-label="Policy summary" aria-busy={isLoading}>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard
          title="Active policies" icon={ShieldCheck} iconTone="success" value={num(s?.active)}
          description={
            !s ? "Policy state unavailable"
              : s.drafts === 0 ? "Every policy is enforcing changes"
              : `${s.drafts} ${plural(s.drafts, "draft is", "drafts are")} not enforcing changes`
          }
        />
        <SummaryCard
          title="Resource coverage" icon={BarChart3} iconTone="success" descriptionTone="success"
          // A percentage of nothing is unknown, not 0%.
          value={s && total > 0 ? ((covered / total) * 100).toFixed(1) + "%" : "\u2014"}
          description={
            !s ? "Coverage unavailable"
              : total === 0 ? "No resources discovered yet"
              : `${covered.toLocaleString("en-US")} of ${total.toLocaleString("en-US")} resources covered`
          }
        />
        <SummaryCard
          title="Open exceptions" icon={TriangleAlert} iconTone="warning" value={num(s?.exceptions.open)}
          descriptionTone={s && s.exceptions.expiring_soon > 0 ? "warning" : undefined}
          description={
            !s ? "Exception state unavailable"
              : s.exceptions.expiring_soon > 0
                ? `${s.exceptions.expiring_soon} expire within the next ${s.exceptions.expiring_window_days} days`
                : `None expire within the next ${s.exceptions.expiring_window_days} days`
          }
        />
        <SummaryCard
          title="Unsafe changes blocked" icon={Ban} iconTone="danger" value={num(s?.blocked_changes)}
          description={`During the ${windowLabel.toLowerCase()}`}
        />
      </div>
    </section>
  );
}