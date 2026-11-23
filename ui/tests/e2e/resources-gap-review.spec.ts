import { expect, test, type Page, type TestInfo } from "../../../tools/ci/node_modules/@playwright/test/index.js";

const now = Date.now();
const assessedAt = new Date(now).toISOString();
const lastReceived = new Date(now - 3 * 60 * 60_000).toISOString();

function assessedResource(id: number, overrides: {
  owner?: string | null;
  telemetry?: "usable" | "stale" | "unmonitored" | "unknown";
  policy?: "covered" | "uncovered" | "unknown";
  actions?: "supported" | "unsupported" | "unknown";
  environment?: string;
  provider?: string;
} = {}) {
  const { owner = "Platform", telemetry = "usable", policy = "covered", actions = "supported",
    environment = "production", provider = "kubernetes" } = overrides;
  return {
    resource: {
      schema_version: 1, id: `test-${id}`, type: "kubernetes.deployment", provider,
      provider_resource_id: `test-${id}`, name: `review-resource-${id}`, environment, owner: owner ?? "",
      region: "example-east", account: "example-account", criticality: "low", lifecycle_state: "active",
      support_status: "fully_supported", labels: { namespace: "example-workloads" }, metadata: {}, current_state: {}, monthly_cost: 0,
      source_plugin_id: "test-discovery", observed_at: assessedAt, first_seen_at: assessedAt,
      last_seen_at: assessedAt, created_at: assessedAt, updated_at: assessedAt,
    },
    assessment: {
      version: "inventory-v1", assessed_at: assessedAt, owner,
      telemetry: {
        status: telemetry, source_plugin_ids: telemetry === "stale" ? ["Test metrics"] : [],
        last_measurement_at: telemetry === "stale" ? lastReceived : null,
        ...(telemetry === "stale" ? { quality: { thresholds: { max_age: "15m" }, dimensions: [{ signal: "CPU usage" }, { signal: "Memory usage" }] } } : {}),
      },
      policy: { status: policy, policy_ids: policy === "covered" ? ["test-policy"] : [] },
      actions: { status: actions }, optimization_status: "no_active_recommendations", active_recommendation_ids: [],
    },
  };
}

const rows = [
  ...Array.from({ length: 6 }, (_, index) => assessedResource(index + 1, { telemetry: "stale" })),
  assessedResource(7, { telemetry: "unmonitored" }),
  assessedResource(8, { owner: null, telemetry: "stale", policy: "uncovered", actions: "unsupported" }),
  assessedResource(9),
  assessedResource(10, { telemetry: "unknown", policy: "unknown", actions: "unknown" }),
  assessedResource(11, { environment: "staging", telemetry: "unmonitored" }),
  assessedResource(12, { provider: "aws", telemetry: "unmonitored" }),
];

async function serveInventory(page: Page, resources: typeof rows, status = 200) {
  await page.route("**/api/resources/inventory?*", (route) => route.fulfill({
    status, headers: { "access-control-allow-origin": "*" },
    json: status === 200 ? {
      resources, filtered_total: resources.length, snapshot_id: "test-resource-snapshot", next_cursor: "", has_more: false,
    } : { title: "Assessment unavailable", status },
  }));
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const path = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

test("selects concern-specific evidence while preserving filters, page and focus", async ({ page }, testInfo) => {
  await serveInventory(page, rows);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1366, height: 700 });
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/resources?provider=kubernetes&environment=production&q=review&view=attention&page=2");
  const views = page.getByRole("group", { name: "Resource inventory views" });
  const attention = views.getByRole("button", { name: /^Attention/ });
  const unmonitored = views.getByRole("button", { name: /^Unmonitored/ });
  const all = views.getByRole("button", { name: /^All/ });
  const table = page.getByRole("table");
  await expect(page.getByText("Coverage requiring attention")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Review all gaps" })).toHaveCount(0);
  await expect(table.getByRole("columnheader", { name: "Gap review" })).toBeVisible();
  await expect(attention.getByText("8", { exact: true })).toBeVisible();
  await expect(attention).toHaveAttribute("aria-pressed", "true");
  await expect(all.getByText("10", { exact: true })).toBeVisible();
  await expect(unmonitored.getByText("1", { exact: true })).toBeVisible();
  await expect(page.getByRole("region", { name: "Resource summary" }).getByText("8", { exact: true })).toBeVisible();
  await expect(table.getByRole("rowheader")).toHaveCount(1);
  const row = table.getByRole("row").filter({ hasText: "review-resource-8" });
  await expect(row.getByRole("cell", { name: "Owner missing" }).getByText("—", { exact: true })).toBeVisible();
  await expect(row.getByRole("cell", { name: "No policy coverage" }).getByText("—", { exact: true })).toBeVisible();
  await expect(row.getByRole("cell").last()).toHaveText("Review details");
  await expect(row.getByRole("link")).toHaveCount(0);
  const review = row.getByRole("button", { name: "Review details for review-resource-8" });
  await review.focus();
  await page.keyboard.press("Enter");

  const dialog = page.getByRole("dialog", { name: "review-resource-8" });
  const content = dialog.getByRole("tabpanel");
  await expect(dialog.getByRole("tab")).toHaveText(["Owner missing", "Stale telemetry", "Policy gap"]);
  await expect(dialog.getByText("Action unsupported", { exact: true })).toHaveCount(0);
  const owner = dialog.getByRole("tab", { name: "Owner missing" });
  await expect(owner).toHaveAttribute("aria-selected", "true");
  await expect(content.locator("dt")).toHaveText(["Owner", "Resource type", "Account", "Last checked"]);
  await expect(dialog.getByText("Namespace", { exact: true })).toHaveCount(0);
  await expect(content.getByRole("listitem")).toHaveCount(1);
  await expect(dialog.getByRole("button", { name: "Check source" })).toBeDisabled();
  await owner.focus();
  await page.keyboard.press("ArrowRight");
  await expect(dialog.getByRole("tab", { name: "Stale telemetry" })).toBeFocused();
  await expect(content.locator("dt")).toHaveText(["Source", "Signals", "Last checked", "Last received", "Maximum report age"]);
  await expect(content.locator(`time[datetime="${assessedAt}"]`)).toHaveCount(1);
  await expect(content.locator(`time[datetime="${lastReceived}"]`)).toHaveCount(1);
  await expect(content.getByText("Test metrics", { exact: true })).toBeVisible();
  await expect(content.getByText("CPU usage and Memory usage", { exact: true })).toBeVisible();
  await expect(content.getByRole("listitem")).toHaveCount(3);
  await expect(dialog.getByRole("button", { name: "Check source" })).toBeDisabled();
  await capture(page, testInfo, "concern-monitoring-light");
  await dialog.getByRole("tab", { name: "Policy gap" }).click();
  await expect(content.locator("dt")).toHaveText(["Policy", "Resource type", "Environment", "Last checked"]);
  await expect(content.getByText("No matching policy", { exact: true })).toBeVisible();
  await expect(content.getByRole("listitem")).toHaveCount(2);
  await expect(dialog.getByRole("button", { name: "Check source" })).toBeDisabled();
  await dialog.getByRole("button", { name: "Recheck", exact: true }).click();
  await expect(dialog.getByRole("status")).toHaveText("Assessment refreshed.");
  await expect(dialog.getByRole("tab", { name: "Policy gap" })).toHaveAttribute("aria-selected", "true");
  const scroll = await page.evaluate(() => window.scrollY);
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(review).toBeFocused();
  await expect(page).toHaveURL(/provider=kubernetes&environment=production&q=review&view=attention&page=2$/);
  expect(await page.evaluate(() => window.scrollY)).toBe(scroll);

  await page.getByRole("button", { name: "Previous" }).click();
  await expect(table.getByRole("rowheader")).toHaveCount(7);
  await page.getByRole("button", { name: "Next" }).click();
  await page.getByRole("button", { name: "Switch to dark theme" }).click();
  await review.click();
  await dialog.getByRole("tab", { name: "Policy gap" }).click();
  await capture(page, testInfo, "concern-policy-dark");
  await dialog.getByRole("button", { name: "Close gap details" }).click();
  await expect(review).toBeFocused();

  await unmonitored.click();
  await expect(page).toHaveURL(/provider=kubernetes&environment=production&q=review&view=unmonitored$/);
  await expect(unmonitored).toHaveAttribute("aria-pressed", "true");
  await expect(table.getByRole("rowheader")).toHaveText([/review-resource-7/]);
  await expect(attention.getByText("8", { exact: true })).toBeVisible();
  await all.click();
  await page.getByRole("button", { name: "Next" }).click();
  for (const id of [9, 10]) {
    const clearRow = table.getByRole("row").filter({ hasText: `review-resource-${id}` });
    await expect(clearRow.getByRole("cell").last()).toHaveText("No action needed");
    await expect(clearRow.getByRole("button")).toHaveCount(0);
  }
  expect(errors).toEqual([]);
});

test("shows an empty inventory and a recoverable assessment error", async ({ page }) => {
  await serveInventory(page, [], 503);
  await page.goto("/resources");
  await expect(page.getByRole("alert").filter({ hasText: "Couldn't load resources" })).toBeVisible();
  await serveInventory(page, []);
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(page.getByText("No resources discovered yet")).toBeVisible();
});

const gapExamples = [
  { key: "owner", changes: { owner: null }, label: "Owner missing", fields: ["Owner", "Resource type", "Account", "Last checked"] },
  { key: "stale", changes: { telemetry: "stale" as const }, label: "Stale telemetry", fields: ["Source", "Signals", "Last checked", "Last received", "Maximum report age"] },
  { key: "unmonitored", changes: { telemetry: "unmonitored" as const }, label: "Unmonitored", fields: ["Monitoring source", "Signals", "Last checked", "Last received", "Owner"] },
  { key: "policy", changes: { policy: "uncovered" as const }, label: "Policy gap", fields: ["Policy", "Resource type", "Environment", "Last checked"] },
];

for (const example of gapExamples) {
  test(`reviews ${example.key} with relevant evidence on laptop and narrow screens`, async ({ page }, testInfo) => {
    await serveInventory(page, [assessedResource(20, example.changes)]);
    await page.setViewportSize({ width: 1366, height: 650 });
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/resources?view=attention");
    const review = page.getByRole("button", { name: "Review details for review-resource-20" });
    const dialog = page.getByRole("dialog", { name: "review-resource-20" });
    for (const theme of ["light", "dark"]) {
      if (theme === "dark") {
        await page.getByRole("button", { name: "Switch to dark theme" }).click();
        await page.setViewportSize({ width: 320, height: 640 });
      }
      await review.click();
      await expect(dialog.getByRole("tab")).toHaveText([example.label]);
      await expect(dialog.getByRole("tabpanel").locator("dt")).toHaveText(example.fields);
      await expect(dialog.getByRole("button", { name: "Check source" })).toBeDisabled();
      await expect(dialog.getByRole("tabpanel").locator(`time[datetime="${assessedAt}"]`)).toHaveCount(1);
      await expect(dialog.getByText("Freshness limit", { exact: true })).toHaveCount(0);
      if (example.key === "unmonitored") {
        await expect(dialog.getByText("Maximum report age", { exact: true })).toHaveCount(0);
        await expect(dialog.getByText("No reports available", { exact: true })).toBeVisible();
      }
      await expect(dialog.getByRole("button", { name: "Recheck", exact: true })).toBeInViewport();
      expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
      if (theme === "light") await capture(page, testInfo, `${example.key}-laptop`);
      await page.keyboard.press("Escape");
      await expect(review).toBeFocused();
    }
  });
}

test("updates the concern tabs after Recheck resolves an issue", async ({ page }) => {
  await serveInventory(page, [assessedResource(30, { owner: null, telemetry: "stale", policy: "uncovered" })]);
  await page.goto("/resources?view=attention");
  await page.getByRole("button", { name: "Review details for review-resource-30" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("tab", { name: "Owner missing" }).click();
  await serveInventory(page, [assessedResource(30, { telemetry: "stale", policy: "uncovered" })]);
  await dialog.getByRole("button", { name: "Recheck", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Owner missing" })).toHaveCount(0);
  await expect(dialog.getByRole("tab", { name: "Stale telemetry" })).toHaveAttribute("aria-selected", "true");
  await serveInventory(page, [assessedResource(30)]);
  await dialog.getByRole("button", { name: "Recheck", exact: true }).click();
  await expect(dialog.getByText("No confirmed coverage concerns remain for this resource.")).toBeVisible();
  await expect(dialog.getByRole("tab")).toHaveCount(0);
  await expect(page.getByRole("group", { name: "Resource inventory views" }).getByRole("button", { name: /^Attention/ }).getByText("0", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Close gap details" }).click();
  await expect(page.getByRole("heading", { name: "Infrastructure inventory", exact: true })).toBeFocused();
});
