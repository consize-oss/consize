import { ResourcesHeader } from "./ResourcesHeader";
import { ResourcesSummary } from "./ResourcesSummary";
import { ResourcesInventoryPanel } from "./ResourcesInventoryPanel";

// Composes the Resources sections. Each section owns its display and connections.
export function ResourcesScreen() {
  return (
    <>
      <ResourcesHeader />
      <ResourcesSummary />
      <ResourcesInventoryPanel />
    </>
  );
}
