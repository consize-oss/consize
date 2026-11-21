"use client";

import { useQueryClient } from "@tanstack/react-query";
import { setScenario, type Scenario } from "@/mocks/handlers";

const SCENARIOS: Scenario[] = [
  "healthy",
  "empty-cluster",
  "no-recommendations",
  "stale",
  "partial-failure",
  "backend-down",
];

// Imported only for explicitly enabled development mocks.

export function ScenarioSwitcher() {
  const queryClient = useQueryClient();

  async function apply(next: Scenario) {
    await queryClient.cancelQueries();
    setScenario(next);
    await queryClient.resetQueries();
  }

  return (
    <label className="flex items-center gap-2 text-xs text-fg-secondary">
      Simulated data · no real changes
      <select
        aria-label="Mock scenario"
        className="h-8 px-2 bg-app-surface border border-app-border rounded-lg text-xs font-mono text-fg"
        defaultValue="healthy"
        onChange={(e) => void apply(e.target.value as Scenario)}
      >
        {SCENARIOS.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </select>
    </label>
  );
}
