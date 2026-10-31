import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { setupServer } from "msw/node";
import { handlers, setScenario } from "@/mocks/handlers";
import { baseRecommendations } from "@/mocks/fixtures";
import { scopeOptionsSchema, recommendationListSchema } from "@/schemas";
import { resourceListSchema, resourceDiscoverySchema, type Resource } from "@/features/resources/queries";
import { baseResources } from "@/mocks/resources";
import {
  buildResourceInventory, resourceInventoryCSV, inventoryDetailsSchema, NO_OWNER,
  type ResourceFilters, type ResourceInventoryDetails,
} from "@/features/resources/resourceInventoryModel";

const empty: ResourceFilters = { q: "", environment: "", owner: "", coverage: "", telemetry: "", optimization: "" };
const exampleTime = "2026-01-01T00:00:00.000Z";

// These examples belong to the tests, independently of the editable demo inventory.
function exampleResource(
  id: string,
  fields: Partial<Resource> = {},
  details: Partial<ResourceInventoryDetails> = {},
): Resource {
  return {
    schema_version: 1, id, name: "Example " + id,
    type: "kubernetes.deployment", provider: "kubernetes", provider_resource_id: "example:" + id,
    environment: "production", owner: "Example Platform", region: "example-east",
    account: "example-account", criticality: "low", lifecycle_state: "active",
    support_status: "fully_supported", labels: {}, current_state: {}, monthly_cost: 100,
    source_plugin_id: "example-kubernetes", observed_at: exampleTime, first_seen_at: exampleTime,
    last_seen_at: exampleTime, created_at: exampleTime, updated_at: exampleTime,
    metadata: { demo_inventory: {
      scope_type: "Deployment", action_path_available: true,
      telemetry: { status: "healthy", source: "Example collector", checked_at: exampleTime },
      policy_coverage: "covered", optimization_status: "no_action_needed", recommendation_ids: [],
      ...details,
    } },
    ...fields,
  };
}

const ruleRecords = [
  exampleResource("healthy"),
  exampleResource("data", { owner: "Example Data" }),
  exampleResource("unowned", { owner: "" }, { policy_coverage: "uncovered" }),
  exampleResource("stale", {}, {
    telemetry: { status: "stale" }, policy_coverage: "uncovered",
    action_path_available: false, optimization_status: "action_unsupported",
  }),
  exampleResource("unmonitored", { owner: "" }, {
    telemetry: { status: "unmonitored" }, action_path_available: false, optimization_status: "action_unsupported",
  }),
  ...Array.from({ length: 5 }, (_, index) => exampleResource("healthy-extra-" + index)),
];

const coverageRecords = [
  ...Array.from({ length: 12 }, (_, index) => exampleResource(
    "coverage-" + index, { owner: index < 3 ? "" : "Example Platform" }, { policy_coverage: "uncovered" },
  )),
  exampleResource("covered-one"),
  exampleResource("covered-two"),
];

describe("Resource inventory rules", () => {
  it("validates string labels while preserving resource metadata and current state", () => {
    const resource = exampleResource("record-maps", {
      labels: { team: "platform" },
      metadata: { provider: { managed: true }, generation: 3 },
      current_state: { replicas: 2, enabled: true, zones: ["east", "west"] },
    });
    const parsed = resourceListSchema.parse({ resources: [resource] }).resources[0];
    assert.deepEqual(parsed.labels, resource.labels);
    assert.deepEqual(parsed.metadata, resource.metadata);
    assert.deepEqual(parsed.current_state, resource.current_state);
    assert.equal(resourceListSchema.safeParse({
      resources: [{ ...resource, labels: { team: 123 } }],
    }).success, false);
  });

  it("counts the full inventory, rather than the seven visible rows", () => {
    const result = buildResourceInventory(ruleRecords, {}, empty, "all", 1);
    assert.equal(result.summary.totalResources, 10);
    assert.equal(result.summary.healthyCount, 8);
    assert.equal(result.summary.healthyPercentage, 80);
    assert.equal(result.summary.actionPathCount, 8);
    assert.equal(result.summary.automationPercentage, 80);
    assert.equal(result.pagination.items.length, 7);
    assert.equal(result.counts.all, 10);
  });

  it("counts an overlapping resource once under Attention and separately under each problem", () => {
    const resource = exampleResource("overlap", { owner: "" }, {
      telemetry: { status: "stale" }, policy_coverage: "uncovered",
      action_path_available: false, optimization_status: "action_unsupported",
    });
    const result = buildResourceInventory([resource], {}, empty, "attention", 1);
    assert.equal(result.counts.attention, 1);
    assert.equal(result.summary.gapCount, 1);
    assert.deepEqual(result.summary.attention, { unowned: 1, stale: 1, policy: 1, unsupported: 1 });
  });

  it("includes unmonitored resources without treating unknown policy as a confirmed gap", () => {
    const unknownPolicy = exampleResource("unknown-policy", {}, { policy_coverage: "unknown" });
    const unmonitored = exampleResource("unmonitored", {}, { telemetry: { status: "unmonitored" } });
    const result = buildResourceInventory([unknownPolicy, unmonitored], {}, empty, "attention", 1);
    assert.equal(result.counts.attention, 1);
    assert.equal(result.counts.unmonitored, 1);
    assert.equal(result.summary.attention.policy, 0);
    assert.equal(result.filteredItems[0].id, "unmonitored");
  });

  it("filters before paginating and keeps tab counts tied to the same matching records", () => {
    const result = buildResourceInventory(coverageRecords, {}, { ...empty, coverage: "uncovered" }, "attention", 2);
    assert.equal(result.counts.all, 12);
    assert.equal(result.counts.attention, 12);
    assert.equal(result.pagination.totalItems, 12);
    assert.equal(result.pagination.totalPages, 2);
    assert.equal(result.pagination.items.length, 5);
    assert.deepEqual(result.pagination.items.map((resource) => resource.id),
      ["coverage-7", "coverage-8", "coverage-9", "coverage-10", "coverage-11"]);
    assert.equal(result.summary.totalResources, 14);
    const unowned = buildResourceInventory(coverageRecords, {}, { ...empty, owner: NO_OWNER }, "all", 8);
    assert.equal(unowned.pagination.currentPage, 1);
    assert.equal(unowned.pagination.totalItems, 3);
    assert.equal(unowned.counts.attention, 3);
  });

  it("searches IDs, names and tags without case sensitivity", () => {
    const records = [
      exampleResource("search-target", { name: "Payment API", labels: { tier: "critical-tier" } }),
      exampleResource("unrelated"),
    ];
    const id = buildResourceInventory(records, {}, { ...empty, q: "SEARCH-TARGET" }, "all", 1);
    assert.deepEqual(id.filteredItems.map((resource) => resource.id), ["search-target"]);
    const tags = buildResourceInventory(records, {}, { ...empty, q: "TIER CRITICAL-TIER" }, "all", 1);
    assert.deepEqual(tags.filteredItems.map((resource) => resource.id), ["search-target"]);
    const missing = buildResourceInventory(records, {}, { ...empty, q: "no-such-resource-xyz" }, "all", 1);
    assert.equal(missing.pagination.totalItems, 0);
    assert.equal(missing.pagination.startItem, 0);
  });

  it("applies infrastructure scope before summaries and keeps dropdown choices stable", () => {
    const records = [
      exampleResource("prod"),
      exampleResource("stage", { environment: "staging" }),
      exampleResource("other-provider", { provider: "aws" }),
      exampleResource("other-type", {}, { scope_type: "Database" }),
    ];
    const scope = { provider: "kubernetes", type: "Deployment" };
    const result = buildResourceInventory(records, scope, empty, "all", 1);
    assert.deepEqual(result.allItems.map((resource) => resource.id), ["prod", "stage"]);
    const narrowed = buildResourceInventory(records, scope, { ...empty, environment: "staging" }, "all", 1);
    assert.equal(narrowed.summary.totalResources, 2);
    assert.deepEqual(narrowed.options.environments, ["production", "staging"]);
    assert.deepEqual(narrowed.filteredItems.map((resource) => resource.id), ["stage"]);
    const bySource = buildResourceInventory(records, { source: "absent-source" }, empty, "all", 1);
    const byRegion = buildResourceInventory(records, { region: "absent-region" }, empty, "all", 1);
    assert.equal(bySource.summary.totalResources, 0);
    assert.equal(byRegion.summary.totalResources, 0);
  });

  it("uses the same Attention rule for the strip, tab and returned resources", () => {
    const result = buildResourceInventory(ruleRecords, {}, empty, "attention", 1);
    assert.equal(result.summary.gapCount, 3);
    assert.equal(result.counts.attention, 3);
    assert.equal(result.pagination.totalItems, 3);
    assert.deepEqual(result.filteredItems.map((resource) => resource.id), ["unowned", "stale", "unmonitored"]);
  });

  it("does not invent rates when assessments or the inventory are unavailable", () => {
    const absent = buildResourceInventory([exampleResource("absent", { metadata: {} })], {}, empty, "all", 1);
    assert.equal(absent.summary.healthyCount, null);
    assert.equal(absent.summary.automationPercentage, null);
    assert.equal(absent.summary.gapCount, null);
    assert.equal(absent.summary.attention.policy, null);
    const none = buildResourceInventory([], {}, empty, "all", 1);
    assert.equal(none.summary.totalResources, 0);
    assert.equal(none.summary.healthyPercentage, null);
    assert.equal(none.summary.automationPercentage, null);
  });

  it("combines additional telemetry and optimization filters before pagination", () => {
    const stale = buildResourceInventory(ruleRecords, {}, { ...empty, telemetry: "stale" }, "all", 1);
    assert.deepEqual(stale.filteredItems.map((resource) => resource.id), ["stale"]);
    const unsupported = buildResourceInventory(ruleRecords, {}, { ...empty, optimization: "action_unsupported" }, "all", 1);
    assert.deepEqual(unsupported.filteredItems.map((resource) => resource.id), ["stale", "unmonitored"]);
    const both = buildResourceInventory(ruleRecords, {}, { ...empty, telemetry: "stale", optimization: "action_unsupported" }, "all", 1);
    assert.deepEqual(both.filteredItems.map((resource) => resource.id), ["stale"]);
    const incompatible = buildResourceInventory(ruleRecords, {}, { ...empty, telemetry: "healthy", optimization: "action_unsupported" }, "all", 1);
    assert.equal(incompatible.pagination.totalItems, 0);
  });

  it("updates totals, percentages and page boundaries as resource and category counts grow", () => {
    const cases = [
      { copies: 1, total: 10, healthy: 8, gaps: 3, pages: 2, lastPageSize: 3 },
      { copies: 2, total: 20, healthy: 16, gaps: 6, pages: 3, lastPageSize: 6 },
      { copies: 10, total: 100, healthy: 80, gaps: 30, pages: 15, lastPageSize: 2 },
      { copies: 100, total: 1000, healthy: 800, gaps: 300, pages: 143, lastPageSize: 6 },
    ];
    for (const example of cases) {
      const records = Array.from({ length: example.copies }, (_, batch) =>
        ruleRecords.map((resource) => ({ ...resource, id: resource.id + "-" + batch })),
      ).flat();
      const result = buildResourceInventory(records, {}, empty, "all", Number.MAX_SAFE_INTEGER);
      assert.equal(result.summary.totalResources, example.total);
      assert.equal(result.counts.all, example.total);
      assert.equal(result.summary.healthyCount, example.healthy);
      assert.equal(result.summary.healthyPercentage, 80);
      assert.equal(result.summary.actionPathCount, example.healthy);
      assert.equal(result.summary.automationPercentage, 80);
      assert.equal(result.summary.gapCount, example.gaps);
      assert.equal(result.counts.attention, example.gaps);
      assert.deepEqual(result.summary.attention, {
        unowned: 2 * example.copies, stale: example.copies,
        policy: 2 * example.copies, unsupported: 2 * example.copies,
      });
      assert.equal(result.counts.unmonitored, example.copies);
      assert.equal(result.pagination.currentPage, example.pages);
      assert.equal(result.pagination.totalPages, example.pages);
      assert.equal(result.pagination.items.length, example.lastPageSize);
      const uncovered = buildResourceInventory(records, {}, { ...empty, coverage: "uncovered" }, "attention", 1);
      assert.equal(uncovered.pagination.totalItems, 2 * example.copies);
    }
  });

  it("updates filter choices and counts when records introduce new owners and environments", () => {
    const records = [
      exampleResource("existing"),
      exampleResource("new-team", { owner: "New Team", environment: "qa" }, {
        telemetry: { status: "stale" }, policy_coverage: "uncovered",
      }),
    ];
    const result = buildResourceInventory(records, {}, { ...empty, owner: "New Team", environment: "qa" }, "all", 1);
    assert.deepEqual(result.options.owners, ["Example Platform", "New Team"]);
    assert.deepEqual(result.options.environments, ["production", "qa"]);
    assert.deepEqual(result.filteredItems.map((resource) => resource.id), ["new-team"]);
    assert.equal(result.summary.healthyPercentage, 50);
    assert.equal(result.counts.attention, 1);
  });

  it("uses the most recent resource update time rather than the time of a read", () => {
    const records = [
      exampleResource("older"),
      exampleResource("newer", { updated_at: "2026-02-01T00:00:00.000Z" }),
    ];
    assert.equal(buildResourceInventory(records, {}, empty, "all", 1).updatedAt, "2026-02-01T00:00:00.000Z");
    assert.equal(buildResourceInventory([], {}, empty, "all", 1).updatedAt, undefined);
  });

  it("exports every matching resource, not only the current seven-row page", () => {
    const result = buildResourceInventory(coverageRecords, {}, { ...empty, coverage: "uncovered" }, "all", 1);
    const csv = resourceInventoryCSV(result.filteredItems);
    assert.equal(result.pagination.items.length, 7);
    assert.equal(result.filteredItems.length, 12);
    assert.ok(csv.startsWith("\ufeff"));
    assert.equal(csv.trimEnd().split("\r\n").length, 13);
    assert.ok(csv.includes("Example coverage-11"));
    assert.ok(!csv.includes("Example covered-one"));
  });

  it("quotes CSV commas, quotes and newlines and keeps formulas as text", () => {
    const item = buildResourceInventory([exampleResource("csv")], {}, empty, "all", 1).filteredItems[0];
    const csv = resourceInventoryCSV([{ ...item, name: "=1+1", owner: 'Team, "A"', inventory: { ...item.inventory, description: "first\nsecond" } }]);
    assert.ok(csv.includes('"' + "'=1+1" + '"'));
    assert.ok(csv.includes('"Team, ""A"""'));
    assert.ok(csv.includes('"first\nsecond"'));
  });
});

describe("Resource demo API responses", () => {
  it("includes every current inventory source/type without changing recommendation records", async () => {
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
      assert.deepEqual(list.items.map((item) => item.id), baseRecommendations.map((item) => item.id));
    } finally {
      setScenario("healthy");
      server.close();
    }
  });

  it("syncs every current demo resource without changing its assessments", async () => {
    const server = setupServer(...handlers);
    server.listen({ onUnhandledRequest: "error" });
    setScenario("healthy");
    try {
      const before = resourceListSchema.parse(await (await fetch("http://127.0.0.1:8080/api/resources")).json());
      assert.deepEqual(before.resources.map((resource) => resource.id), baseResources.map((resource) => resource.id));
      const started = Date.now();
      const response = await fetch("http://127.0.0.1:8080/api/discovery", { method: "POST" });
      assert.equal(response.status, 200);
      const discovery = resourceDiscoverySchema.parse(await response.json());
      assert.equal(discovery.providers.reduce((sum, provider) => sum + provider.count, 0), baseResources.length);
      for (const provider of discovery.providers) {
        assert.equal(provider.count, provider.resources.length);
        assert.ok(provider.resources.every((resource) => resource.source_plugin_id === provider.plugin_id));
      }
      assert.deepEqual(
        discovery.providers.flatMap((provider) => provider.resources.map((resource) => resource.id)).sort(),
        baseResources.map((resource) => resource.id).sort(),
      );
      const list = resourceListSchema.parse(await (await fetch("http://127.0.0.1:8080/api/resources")).json());
      assert.deepEqual(list.resources.map((resource) => resource.id), baseResources.map((resource) => resource.id));
      assert.ok(list.resources.every((resource) => Date.parse(resource.updated_at) >= started));
      assert.deepEqual(list.resources.map((resource) => resource.metadata), before.resources.map((resource) => resource.metadata));
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
