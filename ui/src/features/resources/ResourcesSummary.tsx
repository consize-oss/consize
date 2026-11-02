"use client";

import { Check, Layers, TriangleAlert, Zap } from "lucide-react";
import { SummaryCard } from "@/components/ui/SummaryCard";
import { useResourceInventory } from "./resourceInventory";

export function ResourcesSummary() {
  const { summary, isLoading, isError } = useResourceInventory();
  const available = !isLoading && !isError;
  const number = (value: number | null) => available && value != null ? value.toLocaleString("en-US") : "\u2014";
  const percentage = (value: number | null) => available && value != null ? value.toFixed(1) + "%" : "\u2014";

  return (
    <section aria-label="Resource summary" aria-busy={isLoading}>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard
          title="Discovered resources" icon={Layers} value={number(summary.totalResources)} iconTone="success"
          description={available ? `Across ${summary.sourceCount} connected sources` : "Across connected sources"}
        />
        <SummaryCard
          title="Healthy and reporting" icon={Check} value={number(summary.healthyCount)}
          valueNote={available && summary.healthyPercentage != null ? percentage(summary.healthyPercentage) : undefined}
          iconTone="success" descriptionTone="success" description={available && summary.healthyCount != null ? "Evidence received within policy window" : "Telemetry assessment unavailable"}
        />
        <SummaryCard
          title="Coverage gaps" icon={TriangleAlert} value={number(summary.gapCount)} iconTone="warning" descriptionTone="warning"
          description="Ownership, telemetry, policy, or action attention"
        />
        <SummaryCard
          title="Automation coverage" icon={Zap} value={percentage(summary.automationPercentage)} iconTone="info"
          description={available && summary.actionPathCount != null ? `${summary.actionPathCount} resources have an action path` : "Action-path information unavailable"}
        />
      </div>
    </section>
  );
}
