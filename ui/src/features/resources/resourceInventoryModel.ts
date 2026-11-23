import { z } from "zod";
import type { Resource } from "./queries";
import { getClientPage } from "@/lib/pagination";

export const RESOURCE_PAGE_SIZE = 7;
export const NO_OWNER = "__unassigned__";

export const inventoryDetailsSchema = z.object({
  kind: z.enum(["compute", "database", "storage", "ai", "gpu"]).optional(),
  description: z.string().optional(),
  owner_note: z.string().optional(),
  scope_type: z.string().optional(),
  optimization_status: z.enum(["recommendations", "awaiting_evidence", "no_action_needed", "action_unsupported", "unknown"]).optional(),
  action_path_available: z.boolean().optional(),
  telemetry: z.object({
    status: z.enum(["healthy", "stale", "unmonitored", "unknown"]),
    source: z.string().optional(),
    checked_at: z.string().datetime().optional(),
    freshness_limit: z.string().optional(),
    signals: z.array(z.string()).optional(),
  }).optional(),
  policy_coverage: z.enum(["covered", "uncovered", "unknown"]).optional(),
  policy_ids: z.array(z.string()).optional(),
  assessed_at: z.string().datetime().optional(),
  recommendation_ids: z.array(z.string()).optional(),
});
export type ResourceInventoryDetails = z.infer<typeof inventoryDetailsSchema>;
export type ResourceInventoryItem = Resource & { inventory: ResourceInventoryDetails };
export type ResourceView = "all" | "attention" | "unmonitored";
export type ResourceFilters = { q: string; environment: string; owner: string; coverage: string; telemetry: string; optimization: string };
export type ResourceScope = { provider?: string; source?: string; region?: string; type?: string };

export function readDemoResourceInventory(resource: Resource): ResourceInventoryItem {
  const details = inventoryDetailsSchema.safeParse(resource.metadata.inventory);
  return { ...resource, inventory: details.success ? details.data : {} };
}

export type ResourceConcern = "unowned" | "stale" | "unmonitored" | "policy";

export function resourceConcerns({ owner, inventory }: ResourceInventoryItem): ResourceConcern[] {
  const concerns: ResourceConcern[] = [];
  if (!owner.trim()) concerns.push("unowned");
  if (inventory.telemetry?.status === "stale") concerns.push("stale");
  if (inventory.telemetry?.status === "unmonitored") concerns.push("unmonitored");
  if (inventory.policy_coverage === "uncovered") concerns.push("policy");
  return concerns;
}

export function resourceHasNoGaps({ owner, inventory }: ResourceInventoryItem) {
  return Boolean(owner.trim()) && inventory.telemetry?.status === "healthy" && inventory.policy_coverage === "covered";
}

const unique = (values: string[]) => [...new Set(values)].sort();

export function buildResourceInventory(
  records: readonly ResourceInventoryItem[],
  scope: ResourceScope,
  filters: ResourceFilters,
  view: ResourceView,
  requestedPage: number,
) {
  const allItems = [...new Map(records.map((resource) => [resource.id, resource])).values()];
  const scopedItems = allItems.filter((resource) =>
    (!scope.provider || resource.provider === scope.provider)
    && (!scope.source || resource.source_plugin_id === scope.source)
    && (!scope.region || resource.region === scope.region)
    && (!scope.type || resource.type === scope.type || resource.inventory.scope_type === scope.type),
  );

  const completeTelemetry = scopedItems.every((resource) =>
    resource.inventory.telemetry && resource.inventory.telemetry.status !== "unknown",
  );
  const completeActionPaths = scopedItems.every((resource) =>
    resource.inventory.action_path_available !== undefined,
  );
  const totalResources = scopedItems.length;
  const healthyCount = scopedItems.filter((resource) => resource.inventory.telemetry?.status === "healthy").length;
  const actionPathCount = scopedItems.filter((resource) => resource.inventory.action_path_available === true).length;
  const summary = {
    totalResources,
    sourceCount: new Set(scopedItems.map((resource) => resource.source_plugin_id).filter(Boolean)).size,
    healthyCount: completeTelemetry ? healthyCount : null,
    healthyPercentage: completeTelemetry && totalResources > 0 ? healthyCount / totalResources * 100 : null,
    actionPathCount: completeActionPaths ? actionPathCount : null,
    automationPercentage: completeActionPaths && totalResources > 0 ? actionPathCount / totalResources * 100 : null,
  };

  const terms = filters.q.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const matchingItems = scopedItems.filter((resource) => {
    if (filters.environment && resource.environment !== filters.environment) return false;
    if (filters.telemetry && (resource.inventory.telemetry?.status ?? "unknown") !== filters.telemetry) return false;
    if (filters.optimization && resourceOptimizationStatus(resource) !== filters.optimization) return false;
    if (filters.owner === NO_OWNER ? Boolean(resource.owner.trim()) : filters.owner && resource.owner !== filters.owner) return false;
    if (filters.coverage && (resource.inventory.policy_coverage ?? "unknown") !== filters.coverage) return false;
    const searchable = [
      resource.id, resource.provider_resource_id, resource.name, resource.owner, resource.account,
      resource.inventory.owner_note, resource.inventory.description,
      ...Object.keys(resource.labels), ...Object.values(resource.labels),
    ].join(" ").toLowerCase();
    return terms.every((term) => searchable.includes(term));
  });
  const attentionItems = matchingItems.filter((resource) => resourceConcerns(resource).length > 0);
  const unmonitoredItems = attentionItems.filter((resource) => resource.inventory.telemetry?.status === "unmonitored");
  const counts = {
    all: matchingItems.length,
    attention: attentionItems.length,
    unmonitored: unmonitoredItems.length,
  };
  const filteredItems = view === "attention" ? attentionItems : view === "unmonitored" ? unmonitoredItems : matchingItems;

  return {
    updatedAt: scopedItems.reduce<string | undefined>((latest, resource) =>
      !latest || Date.parse(resource.updated_at) > Date.parse(latest) ? resource.updated_at : latest, undefined),
    allItems: scopedItems,
    filteredItems,
    summary: { ...summary, gapCount: counts.attention },
    counts,
    options: {
      environments: unique(scopedItems.map((resource) => resource.environment)),
      owners: [
        ...(scopedItems.some((resource) => !resource.owner.trim()) ? [NO_OWNER] : []),
        ...unique(scopedItems.map((resource) => resource.owner).filter((owner) => owner.trim())),
      ],
      coverage: ["covered", "uncovered", "unknown"],
      telemetry: unique(scopedItems.map((resource) => resource.inventory.telemetry?.status ?? "unknown")),
      optimization: unique(scopedItems.map(resourceOptimizationStatus)),
    },
    pagination: getClientPage(filteredItems, requestedPage, RESOURCE_PAGE_SIZE),
  };
}


export const TELEMETRY_LABELS = {
  healthy: "Healthy", stale: "Stale", unmonitored: "Unmonitored", unknown: "Unknown",
} as const;
export const OPTIMIZATION_LABELS = {
  recommendations: "Recommendations", awaiting_evidence: "Awaiting evidence",
  no_action_needed: "No action needed", action_unsupported: "Action unsupported", unknown: "Unknown",
} as const;

export function resourceOptimizationStatus(resource: ResourceInventoryItem) {
  return resource.inventory.optimization_status
    ?? (resource.inventory.recommendation_ids?.length ? "recommendations" : "unknown");
}

function csvCell(value: string) {
  // Quote cells and keep user-provided text from being interpreted as spreadsheet formulas.
  const text = /^[\s]*[=+\-@]/.test(value) || /^[\t\r\n]/.test(value) ? "'" + value : value;
  return '"' + text.replaceAll('"', '""') + '"';
}

export function resourceInventoryCSV(items: readonly ResourceInventoryItem[]) {
  const rows = [
    ["Resource ID", "Resource", "Description", "Provider", "Environment", "Owner", "Owner details",
      "Telemetry", "Telemetry source", "Telemetry checked at", "Policy", "Optimization status"],
    ...items.map((resource) => {
      const details = resource.inventory;
      const status = resourceOptimizationStatus(resource);
      const count = details.recommendation_ids?.length;
      const optimization = status === "recommendations" && count != null
        ? count + (count === 1 ? " recommendation" : " recommendations") : OPTIMIZATION_LABELS[status];
      return [
        resource.id, resource.name, details.description ?? resource.type,
        resource.provider === "aws" ? "AWS" : resource.provider === "kubernetes" ? "Kubernetes" : resource.provider,
        resource.environment === "datalake" ? "DataLake" : resource.environment,
        resource.owner, details.owner_note ?? "",
        TELEMETRY_LABELS[details.telemetry?.status ?? "unknown"],
        details.telemetry?.source ?? "", details.telemetry?.checked_at ?? "",
        details.policy_coverage === "covered" ? "Covered" : "", optimization,
      ];
    }),
  ];
  return "\ufeff" + rows.map((row) => row.map(csvCell).join(",")).join("\r\n") + "\r\n";
}
