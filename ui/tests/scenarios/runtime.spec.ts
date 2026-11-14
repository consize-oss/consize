import {
  expect,
  test,
} from "../../../tools/ci/node_modules/@playwright/test/index.js";

test("development chooser drives the runtime screens without a running API", async ({
  page,
}) => {
  const responses: boolean[] = [];
  const realApiRequests: string[] = [];
  page.on("request", request => { if (request.url().startsWith("http://127.0.0.1:18089")) realApiRequests.push(request.url()); });
  page.on("response", (response) => {
    if (response.url().includes("consize-mock.invalid/api/"))
      responses.push(response.fromServiceWorker());
  });
  await page.goto("/");
  const chooser = page.getByRole("combobox", { name: "Mock scenario" });
  await expect(chooser).toBeVisible();
  await expect(
    page.getByText("Simulated data · no real changes", { exact: false }),
  ).toBeVisible();
  await expect(page.getByRole("main").getByRole("alert")).toHaveCount(0);
  await chooser.selectOption("empty-cluster");
  await expect(
    page.getByText("No recommendations yet.", { exact: true }),
  ).toBeVisible();
  await chooser.selectOption("healthy");
  await expect(
    page.getByRole("link", { name: "payment-service-deployment", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("link", { name: "payment-service-deployment", exact: true })
    .click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "payment-service",
  );
  await page.getByRole("tab", { name: "Evidence", exact: true }).click();
  await expect(page.getByRole("table")).toBeVisible();
  await chooser.selectOption("stale");
  await expect(
    page.getByRole("cell", { name: "Stale", exact: true }).first(),
  ).toBeVisible();
  await page.goto("/plugins");
  await expect(
    page.getByText("Prometheus Metrics (simulated)", { exact: true }).first(),
  ).toBeVisible();
  await chooser.selectOption("partial-failure");
  await expect(
    page.getByText("Degraded", { exact: false }).first(),
  ).toBeVisible();
  await chooser.selectOption("backend-down");
  await expect(page.getByRole("main").getByRole("alert").first()).toBeVisible();
  await chooser.selectOption("healthy");
  await expect(page.getByRole("main").getByRole("alert")).toHaveCount(0);
  const status = await page.evaluate(
    async () =>
      (
        await fetch("https://consize-mock.invalid/api/unimplemented-command", {
          method: "POST",
        })
      ).status,
  );
  expect(status).toBe(501);
  expect(responses.length).toBeGreaterThan(0);
  expect(responses.every(Boolean)).toBeTruthy();
  expect(realApiRequests).toEqual([]);
  // Losing the worker must fail locally, never use the configured real API.
  await page.route("https://consize-mock.invalid/**", route => route.abort("connectionrefused"));
  await page.evaluate(() => {
    const runtime = (globalThis as typeof globalThis & { __consizeMockWorker?: { worker: { stop(): void } } }).__consizeMockWorker;
    if (!runtime) throw new Error("Mock worker was not initialized");
    runtime.worker.stop();
  });
  await chooser.selectOption("backend-down");
  await expect(page.getByRole("main").getByRole("alert").first()).toBeVisible();
  expect(realApiRequests).toEqual([]);
});

test("simulated apply uses a dry-run and confirmation, then shows mock job and audit", async ({
  page,
}) => {
  await page.goto("/recommendations/1");
  await page.getByRole("button", { name: "Review dry-run plan" }).click();
  await page
    .getByRole("button", { name: "Approve and apply", exact: true })
    .click();
  const confirmation = page.getByRole("dialog", {
    name: "Apply this infrastructure change?",
  });
  await expect(confirmation).toBeVisible();
  await confirmation.getByRole("button", { name: "Confirm apply" }).click();
  await expect(page.getByText("verifying", { exact: true })).toBeVisible();
  await expect(page.getByText("verified", { exact: true })).toBeVisible({
    timeout: 15_000,
  });
  await page.getByRole("tab", { name: "Audit", exact: true }).click();
  await expect(
    page.getByText("Simulated workflow: verified", { exact: true }),
  ).toBeVisible();
});

test("populated Overview and Resources keep every approved column visible on desktop", async ({page}, info) => {
  for (const theme of ["light", "dark"]) {
  await page.addInitScript(theme => localStorage.setItem("consize-theme", theme), theme);
  for (const width of [1280, 1440, 1920]) {
    await page.setViewportSize({width, height:900});
    for (const view of [
      {path:"/",region:"Prioritized recommendations table",columns:["Resource","Owner / policy","Change risk","Est. savings","Action"]},
      {path:"/resources",region:"Resources table",columns:["Resource","Provider","Environment","Owner","Telemetry","Policy","Optimization status"]},
    ]) {
      await page.goto(view.path);
      const region=page.getByRole("region",{name:view.region,exact:true});
      await expect(region).toBeVisible();
      await expect(region.getByRole("columnheader")).toHaveText(view.columns);
      expect(await region.evaluate(element=>element.scrollWidth<=element.clientWidth+1)).toBeTruthy();
      const bounds=await region.boundingBox();expect(bounds).not.toBeNull();
      for (const header of await region.getByRole("columnheader").all()) {
        const box=await header.boundingBox();expect(box).not.toBeNull();
        expect(box!.x).toBeGreaterThanOrEqual(bounds!.x-1);
        expect(box!.x+box!.width).toBeLessThanOrEqual(bounds!.x+bounds!.width+1);
      }
      await info.attach(`${view.path==="/"?"overview":"resources"}-populated-${theme}-${width}`,{body:await page.screenshot({fullPage:true}),contentType:"image/png"});
    }
  }
  }
});
