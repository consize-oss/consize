import { after, before, beforeEach, it } from "node:test";
import assert from "node:assert/strict";
import { setupServer } from "msw/node";
import { handlers, setScenario, type Scenario } from "../mocks/handlers";
import {
  readDashboard,
  recommendationViews,
  detailView,
  pluginsView,
  runtimePlanSchema,
  runtimeAcceptedSchema,
  actionEventsSchema,
  canApprovePlan,
} from "./runtime-api";
import { apiGet, apiPost } from "./api-client";
import {
  resourceListSchema,
  resourceDiscoverySchema,
} from "../features/resources/queries";
const server = setupServer(...handlers);
before(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => setScenario("healthy"));
after(() => {
  setScenario("healthy");
  server.close();
});

for (const scenario of [
  "healthy",
  "empty-cluster",
  "no-recommendations",
  "stale",
  "partial-failure",
] as Scenario[]) {
  it(`${scenario} supplies the validated runtime API used by production screens`, async () => {
    setScenario(scenario);
    const data = await readDashboard();
    assert.equal(data.resources.length === 0, scenario === "empty-cluster");
    assert.equal(
      data.recommendations.length === 0,
      ["empty-cluster", "no-recommendations"].includes(scenario),
    );
    assert.ok(pluginsView(data).items.length > 0);
    const list = recommendationViews(data);
    if (list.items.length && scenario !== "partial-failure") {
      const detail = await detailView(list.items[0].id);
      assert.equal(detail.resource.id, list.items[0].resource.id);
      if (scenario === "stale")
        assert.ok(detail.evidence.every((e) => e.freshness === "stale"));
    }
  });
}
it("backend-down fails runtime reads and commands without reaching a real server", async () => {
  setScenario("backend-down");
  for (const path of [
    "/api/dashboard",
    "/api/resources",
    "/api/jobs",
    "/api/actions",
    "/api/plugins",
  ]) {
    const response = await fetch(`http://127.0.0.1:8080${path}`);
    assert.equal(response.status, 503, path);
  }
  await assert.rejects(
    apiPost(
      "/api/recommendations/1/execute",
      runtimeAcceptedSchema,
      undefined,
      { mode: "approved" },
    ),
    { status: 503 },
  );
});
it("partial failure retains dashboard data and degraded health while affected services fail", async () => {
  setScenario("partial-failure");
  const data = await readDashboard();
  assert.ok(data.resources.length > 0);
  assert.ok(data.plugins.some((p) => p.health.status === "degraded"));
  assert.equal((await fetch("http://127.0.0.1:8080/api/actions")).status, 503);
  assert.equal(
    (await fetch("http://127.0.0.1:8080/api/resources")).status,
    503,
  );
});
it("simulated evaluation, planning, approval, job polling and audit agree", async () => {
  const initial = await readDashboard();
  const id = initial.recommendations.find((r) => r.status === "pending")!.id;
  const result = await apiPost(
    `/api/recommendations/${id}/plan`,
    runtimePlanSchema,
  );
  assert.equal(canApprovePlan(result.planned), true);
  await assert.rejects(
    apiPost(`/api/recommendations/${id}/execute`, runtimeAcceptedSchema),
    { status: 400 },
  );
  const submitted = await apiPost(
    `/api/recommendations/${id}/execute`,
    runtimeAcceptedSchema,
    undefined,
    { mode: "approved" },
  );
  assert.equal(submitted.job.state, "prepared");
  const duplicate = await apiPost(
    `/api/recommendations/${id}/execute`,
    runtimeAcceptedSchema,
    undefined,
    { mode: "approved" },
  );
  assert.equal(duplicate.job.id, submitted.job.id);
  const first = await fetch("http://127.0.0.1:8080/api/jobs");
  assert.equal((await first.json()).jobs[0].state, "verifying");
  const second = await fetch("http://127.0.0.1:8080/api/jobs");
  assert.equal((await second.json()).jobs[0].state, "verified");
  const events = await apiGet("/api/actions", actionEventsSchema);
  assert.deepEqual(
    events.actions.map((e) => e.result),
    ["prepared", "verifying", "verified"],
  );
  assert.equal(
    recommendationViews(await readDashboard()).items.find(
      (r) => r.id === String(id),
    )?.workflow_state,
    "verified",
  );
  const resourceId = initial.resources[0].id;
  const evaluation = await fetch(
    `http://127.0.0.1:8080/api/recommendations/generate?${new URLSearchParams({ resource_id: resourceId })}`,
    { method: "POST" },
  );
  assert.equal(evaluation.status, 200);
  assert.equal(
    (await readDashboard()).recommendations.length,
    initial.recommendations.length + 1,
  );
});
it("stale evidence blocks simulated mutation; switching scenarios resets jobs and recommendations", async () => {
  let data = await readDashboard();
  const id = data.recommendations.find((r) => r.status === "pending")!.id;
  await apiPost(
    `/api/recommendations/${id}/execute`,
    runtimeAcceptedSchema,
    undefined,
    { mode: "approved" },
  );
  setScenario("stale");
  const planned = await apiPost(
    `/api/recommendations/${id}/plan`,
    runtimePlanSchema,
  );
  assert.equal(canApprovePlan(planned.planned), false);
  await assert.rejects(
    apiPost(
      `/api/recommendations/${id}/execute`,
      runtimeAcceptedSchema,
      undefined,
      { mode: "approved" },
    ),
    { status: 400 },
  );
  setScenario("no-recommendations");
  data = await readDashboard();
  assert.equal(data.jobs.length, 0);
  assert.equal(data.recommendations.length, 0);
  setScenario("healthy");
  data = await readDashboard();
  assert.equal(data.jobs.length, 0);
  assert.ok(data.recommendations.length > 0);
});
it("discovery and resource links use the same runtime recommendation IDs", async () => {
  const before = await apiGet("/api/resources", resourceListSchema);
  const data = await readDashboard();
  assert.deepEqual(
    before.resources.map((r) => r.id),
    data.resources.map((r) => r.id),
  );
  const sync = await apiPost("/api/discovery", resourceDiscoverySchema);
  assert.ok(sync.providers.length > 0);
  const detail = await detailView(String(data.recommendations[0].id));
  assert.equal(detail.id, String(data.recommendations[0].id));
});
it("unimplemented API commands and missing recommendations never pass through", async () => {
  assert.equal(
    (
      await fetch("http://127.0.0.1:8080/api/not-implemented", {
        method: "POST",
      })
    ).status,
    501,
  );
  await assert.rejects(
    apiPost("/api/recommendations/999999/plan", runtimePlanSchema),
    { status: 404 },
  );
});
