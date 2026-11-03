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

  function apply(next: Scenario) {
    setScenario(next);
    void queryClient.invalidateQueries();
  }

  return (
    <label className="flex items-center gap-2 text-xs text-fg-secondary">
      Demo data
    <select
      aria-label="Mock scenario"
      className="h-8 px-2 bg-app-surface border border-app-border rounded-lg text-xs font-mono text-slate-300"
      defaultValue="healthy"
      onChange={(e) => apply(e.target.value as Scenario)}
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