import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { setupServer } from "msw/node";
import { handlers, setScenario } from "@/mocks/handlers";
import { scopeOptionsSchema, recommendationListSchema } from "@/schemas";
import { resourceListSchema, resourceDiscoverySchema } from "@/features/resources/queries";
import { baseResources } from "@/mocks/resources";
import {
  buildResourceInventory, resourceInventoryCSV, inventoryDetailsSchema, NO_OWNER, resourceNeedsAttention, type ResourceFilters,
} from "@/features/resources/resourceInventoryModel";

const empty: ResourceFilters = { q: "", environment: "", owner: "", coverage: "", telemetry: "", optimization: "" };

describe("Resource inventory rules", () => {
  it("counts the full inventory, rather than the seven visible rows", () => {
    const result = buildResourceInventory(baseResources, {}, empty, "all", 1);
    assert.equal(result.summary.totalResources, 50);
    assert.equal(result.summary.healthyCount, 39);
    assert.equal(result.summary.healthyPercentage, 78);
    assert.equal(result.summary.actionPathCount, 43);
    assert.equal(result.summary.automationPercentage, 86);
    assert.equal(result.pagination.items.length, 7);
    assert.equal(result.counts.all, 50);
  });

  it("counts an overlapping resource once under Attention and separately under each problem", () => {
    const resource = { ...baseResources[0], owner: "", metadata: { demo_inventory: {
      telemetry: { status: "stale" }, policy_coverage: "uncovered", action_path_available: false,
    } } };
    const result = buildResourceInventory([resource], {}, empty, "attention", 1);
    assert.equal(result.counts.attention, 1);
    assert.equal(result.summary.gapCount, 1);
    assert.deepEqual(result.summary.attention, { unowned: 1, stale: 1, policy: 1, unsupported: 1 });
  });

  it("includes unmonitored resources without treating unknown policy as a confirmed gap", () => {
    const unknownPolicy = { ...baseResources[0], metadata: { demo_inventory: {
      telemetry: { status: "healthy" }, policy_coverage: "unknown", action_path_available: true,
    } } };
    const unmonitored = { ...unknownPolicy, id: "unmonitored", metadata: { demo_inventory: {
      telemetry: { status: "unmonitored" }, policy_coverage: "covered", action_path_available: true,
    } } };
    const result = buildResourceInventory([unknownPolicy, unmonitored], {}, empty, "attention", 1);
    assert.equal(result.counts.attention, 1);
    assert.equal(result.counts.unmonitored, 1);
    assert.equal(result.summary.attention.policy, 0);
    assert.equal(result.filteredItems[0].id, "unmonitored");
  });

  it("filters before paginating and keeps tab counts tied to the same matching records", () => {
    const result = buildResourceInventory(baseResources, {}, { ...empty, coverage: "uncovered" }, "attention", 2);
    assert.equal(result.counts.all, 12);
    assert.equal(result.counts.attention, 12);
    assert.equal(result.pagination.totalItems, 12);
    assert.equal(result.pagination.totalPages, 2);
    assert.equal(result.pagination.items.length, 5);
    assert.ok(result.filteredItems.every((resource) => resource.inventory.policy_coverage === "uncovered"));
    assert.equal(result.summary.totalResources, 50);
    const unowned = buildResourceInventory(baseResources, {}, { ...empty, owner: NO_OWNER }, "all", 8);
    assert.equal(unowned.pagination.currentPage, 1);
    assert.equal(unowned.pagination.totalItems, 5);
    assert.equal(unowned.counts.attention, 5);
  });

  it("searches IDs, names and tags without case sensitivity", () => {
    const id = buildResourceInventory(baseResources, {}, { ...empty, q: "RES_14" }, "all", 1);
    assert.deepEqual(id.filteredItems.map((resource) => resource.id), ["res_14"]);
    const tags = buildResourceInventory(baseResources, {}, { ...empty, q: "tier 1" }, "all", 1);
    assert.ok(tags.filteredItems.some((resource) => resource.id === "res_01"));
    const missing = buildResourceInventory(baseResources, {}, { ...empty, q: "no-such-resource-xyz" }, "all", 1);
    assert.equal(missing.pagination.totalItems, 0);
    assert.equal(missing.pagination.startItem, 0);
  });

  it("applies infrastructure scope before summaries and keeps dropdown choices stable", () => {
    const scope = { provider: "kubernetes", type: "Deployment" };
    const result = buildResourceInventory(baseResources, scope, empty, "all", 1);
    assert.ok(result.summary.totalResources > 7);
    assert.ok(result.allItems.every((resource) => resource.provider === "kubernetes" && resource.inventory.scope_type === "Deployment"));
    const narrowed = buildResourceInventory(baseResources, scope, { ...empty, environment: "staging" }, "all", 1);
    assert.equal(narrowed.summary.totalResources, result.summary.totalResources);
    assert.deepEqual(narrowed.options.environments, result.options.environments);
    assert.ok(narrowed.filteredItems.every((resource) => resource.environment === "staging"));
  });

  it("uses the same Attention rule for the strip, tab and returned resources", () => {
    const result = buildResourceInventory(baseResources, {}, empty, "attention", 1);
    assert.equal(result.summary.gapCount, result.counts.attention);
    assert.equal(result.pagination.totalItems, result.counts.attention);
    assert.ok(result.filteredItems.every(resourceNeedsAttention));
    assert.equal(new Set(result.filteredItems.map((resource) => resource.id)).size, result.filteredItems.length);
  });

  it("does not invent rates when assessments or the inventory are unavailable", () => {
    const absent = buildResourceInventory([{ ...baseResources[0], metadata: {} }], {}, empty, "all", 1);
    assert.equal(absent.summary.healthyCount, null);
    assert.equal(absent.summary.automationPercentage, null);
    assert.equal(absent.summary.gapCount, null);
    assert.equal(absent.summary.attention.policy, null);
    const none = buildResourceInventory([], {}, empty, "all", 1);
    assert.equal(none.summary.totalResources, 0);
    assert.equal(none.summary.healthyPercentage, null);
    assert.equal(none.summary.automationPercentage, null);
  });
  it("includes every inventory source/type in the shared mock scope options without changing recommendations", async () => {
    const server = setupServer(...handlers);
    server.listen({ onUnhandledRequest: "error" });
    setScenario("healthy");
    try {
      const response = await fetch("http://127.0.0.1:8080/api/v1/scope-options");
      assert.equal(response.status, 200);
      const options = scopeOptionsSchema.parse(await response.json());
      for (const resource of baseResources) {
        const details = inventoryDetailsSchema.parse(resource.metadata.demo_inventory);
        assert.ok(options.providers.some((option) => option.value === resource.provider));
        assert.ok(options.sources.some((option) => option.value === resource.source_plugin_id));
        assert.ok(options.regions.some((option) => option.value === resource.region));
        assert.ok(options.resource_types.some((option) => option.value === (details.scope_type ?? resource.type)));
      }
      const recommendations = await fetch("http://127.0.0.1:8080/api/v1/recommendations");
      const list = recommendationListSchema.parse(await recommendations.json());
      assert.equal(list.items.length, 13);
    } finally {
      setScenario("healthy");
      server.close();
    }
  });

  it("combines additional telemetry and optimization filters before pagination", () => {
    const stale = buildResourceInventory(baseResources, {}, { ...empty, telemetry: "stale" }, "all", 1);
    assert.equal(stale.pagination.totalItems, 7);
    const unsupported = buildResourceInventory(baseResources, {}, { ...empty, optimization: "action_unsupported" }, "all", 1);
    assert.equal(unsupported.pagination.totalItems, 7);
    const both = buildResourceInventory(baseResources, {}, { ...empty, telemetry: "stale", optimization: "action_unsupported" }, "all", 1);
    assert.equal(both.pagination.totalItems, 0);
  });

  it("uses the most recent resource update time rather than the time of a read", () => {
    const records = [
      { ...baseResources[0], updated_at: "2026-01-01T00:00:00.000Z" },
      { ...baseResources[1], updated_at: "2026-02-01T00:00:00.000Z" },
    ];
    assert.equal(buildResourceInventory(records, {}, empty, "all", 1).updatedAt, "2026-02-01T00:00:00.000Z");
    assert.equal(buildResourceInventory([], {}, empty, "all", 1).updatedAt, undefined);
  });

  it("exports every matching resource, not only the current seven-row page", () => {
    const result = buildResourceInventory(baseResources, {}, { ...empty, coverage: "uncovered" }, "all", 1);
    const csv = resourceInventoryCSV(result.filteredItems);
    assert.equal(result.pagination.items.length, 7);
    assert.equal(result.filteredItems.length, 12);
    assert.ok(csv.startsWith("\ufeff"));
    assert.equal(csv.trimEnd().split("\r\n").length, 13);
    assert.ok(csv.includes(result.filteredItems[11].name));
  });

  it("quotes CSV commas, quotes and newlines and keeps formulas as text", () => {
    const item = buildResourceInventory(baseResources, {}, empty, "all", 1).filteredItems[0];
    const csv = resourceInventoryCSV([{ ...item, name: "=1+1", owner: 'Team, "A"', inventory: { ...item.inventory, description: "first\nsecond" } }]);
    assert.ok(csv.includes('"' + "'=1+1" + '"'));
    assert.ok(csv.includes('"Team, ""A"""'));
    assert.ok(csv.includes('"first\nsecond"'));
  });

  it("syncs the demo inventory through the existing discovery contract without clearing gaps", async () => {
    const server = setupServer(...handlers);
    server.listen({ onUnhandledRequest: "error" });
    setScenario("healthy");
    try {
      const started = Date.now();
      const response = await fetch("http://127.0.0.1:8080/api/discovery", { method: "POST" });
      assert.equal(response.status, 200);
      const discovery = resourceDiscoverySchema.parse(await response.json());
      assert.equal(discovery.providers.reduce((sum, provider) => sum + provider.count, 0), 50);
      const list = resourceListSchema.parse(await (await fetch("http://127.0.0.1:8080/api/resources")).json());
      assert.ok(list.resources.every((resource) => Date.parse(resource.updated_at) >= started));
      const inventory = buildResourceInventory(list.resources, {}, empty, "all", 1);
      assert.equal(inventory.summary.gapCount, 24);
      assert.equal(inventory.summary.healthyCount, 39);
    } finally {
      setScenario("healthy");
      server.close();
    }
  });

  it("does not update the inventory time when discovery fails", async () => {
    const server = setupServer(...handlers);
    server.listen({ onUnhandledRequest: "error" });
    setScenario("healthy");
    try {
      const before = resourceListSchema.parse(await (await fetch("http://127.0.0.1:8080/api/resources")).json());
      setScenario("backend-down");
      assert.equal((await fetch("http://127.0.0.1:8080/api/discovery", { method: "POST" })).status, 503);
      setScenario("healthy");
      const after = resourceListSchema.parse(await (await fetch("http://127.0.0.1:8080/api/resources")).json());
      assert.deepEqual(after.resources.map((resource) => resource.updated_at), before.resources.map((resource) => resource.updated_at));
    } finally {
      setScenario("healthy");
      server.close();
    }
  });
});
