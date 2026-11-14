import {
  expect,
  test,
} from "../../../tools/ci/node_modules/@playwright/test/index.js";
import AxeBuilder from "../../../tools/ci/node_modules/@axe-core/playwright/dist/index.js";

test("real API empty startup, filters, theme, disconnection and recovery", async ({
  page,
  request,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  expect(
    (await request.get("http://127.0.0.1:18080/api/health")).ok(),
  ).toBeTruthy();
  await page.goto("/");
  await expect(page.getByRole("combobox", { name: "Mock scenario" })).toHaveCount(0);
  await expect(
    page.getByText("No recommendations yet.", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("main").getByRole("alert")).toHaveCount(0);
  await expect(
    page.getByText("No applied changes yet", { exact: true }),
  ).toBeVisible();
  const original = await page.locator("html").getAttribute("data-theme");
  await page
    .getByRole("button", { name: /Switch to (dark|light) theme/ })
    .click();
  await expect(page.locator("html")).toHaveAttribute(
    "data-theme",
    original === "dark" ? "light" : "dark",
  );
  await page
    .getByRole("navigation", { name: "Primary" })
    .getByRole("link", { name: "Recommendations", exact: true })
    .click();
  const search = page.getByRole("searchbox", {
    name: "Search recommendations",
  });
  await search.fill("synthetic");
  await search.press("Enter");
  await expect(page).toHaveURL(/q=synthetic/);
  await page.getByRole("button", { name: "Reset Filters" }).click();
  await expect(search).toHaveValue("");
  await page.route("**/api/dashboard", (route) =>
    route.abort("connectionrefused"),
  );
  await page.reload();
  await expect(
    page
      .getByRole("main")
      .getByRole("alert")
      .filter({ hasText: "Couldn't load recommendations" }),
  ).toBeVisible();
  await page.unroute("**/api/dashboard");
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page
      .getByRole("main")
      .getByRole("alert")
      .filter({ hasText: "Couldn't load recommendations" }),
  ).toHaveCount(0);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Recommendations", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      async () => (await navigator.serviceWorker.getRegistrations()).length,
    ),
  ).toBe(0);
  expect(errors).toEqual([]);
});
for (const theme of ["light", "dark"] as const) {
  for (const width of [390, 768, 1280, 1440, 1920]) {
    test(`real empty routes: ${theme} at ${width}px`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 });
      await page.addInitScript(
        (theme) => localStorage.setItem("consize-theme", theme),
        theme,
      );
      for (const path of [
        "/",
        "/resources",
        "/recommendations",
        "/plugins",
        "/policies",
      ]) {
        await page.goto(path);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await expect(
          page.locator('[aria-label="Loading overview"]'),
        ).toHaveCount(0);
        await expect(page.getByRole("main").getByRole("alert")).toHaveCount(0);
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= window.innerWidth,
          ),
        ).toBeTruthy();
        const scan = await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
          .analyze();
        expect(
          scan.violations,
          JSON.stringify(
            scan.violations.map((x) => ({
              id: x.id,
              nodes: x.nodes.map((n) => n.target),
            })),
          ),
        ).toEqual([]);
        await info.attach(
          `${path.replaceAll("/", "") || "overview"}-${theme}-${width}`,
          {
            body: await page.screenshot({ fullPage: true }),
            contentType: "image/png",
          },
        );
      }
    });
  }
}

test("registered inventory survives reload and mobile navigation supports Escape", async ({
  page,
  request,
}) => {
  const name =
    "browser-registered-resource-with-a-long-name-for-layout-validation";
  const now = new Date().toISOString();
  const response = await request.post("http://127.0.0.1:18080/api/resources", {
    data: {
      id: "browser-registered",
      type: "kubernetes.deployment",
      provider: "kubernetes",
      provider_resource_id: "test/browser-registered",
      name,
      environment: "development",
      owner: "test-owner",
      region: "local",
      account: "test-cluster",
      criticality: "low",
      labels: {},
      metadata: {},
      current_state: {},
      monthly_cost: 0,
      source_plugin_id: "test-discovery",
      observed_at: now,
      created_at: now,
      updated_at: now,
    },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  await page.goto("/resources");
  await expect(page.getByRole("row").filter({ hasText: name })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("row").filter({ hasText: name })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 900 });
  await page.getByRole("button", { name: "Open navigation" }).click();
  const navigation = page.getByRole("dialog", { name: "Navigation" });
  await expect(navigation).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(navigation).not.toBeVisible();
  await expect(
    page.getByRole("button", { name: "Open navigation" }),
  ).toBeFocused();
  const scan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(scan.violations).toEqual([]);
});
