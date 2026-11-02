import { expect, test } from "../../../tools/ci/node_modules/@playwright/test/index.js";

test("real API reads and visible UI contract failure without mock data", async ({ page, request }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  const api = "http://127.0.0.1:18080";
  expect((await request.get(`${api}/api/health`)).ok()).toBeTruthy();
  const resources = await request.get(`${api}/api/resources`);
  expect(resources.ok()).toBeTruthy();
  expect((await resources.json()).resources ?? []).toEqual([]);
  const summaryRequest = page.waitForRequest(request =>
    request.url().startsWith(`${api}/api/v1/dashboard/summary`));
  await page.goto("/");
  // The current UI contract is not yet implemented by the Go API. Assert its
  // real failure state explicitly; this smoke must not invent a happy path.
  await summaryRequest;
  expect((await request.get(`${api}/api/v1/dashboard/summary`)).status()).toBe(404);
  await expect(page.getByRole("alert").filter({ hasText: "Couldn't load summary metrics" })).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "Couldn't load recommendations" })).toBeVisible();
  const originalTheme = await page.locator("html").getAttribute("data-theme");
  await page.getByRole("button", { name: /Switch to (dark|light) theme/ }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", originalTheme === "dark" ? "light" : "dark");
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Recommendations", exact: true }).click();
  await expect(page).toHaveURL(/\/recommendations/);
  await expect(page.getByRole("heading", { name: "Recommendations", exact: true })).toBeVisible();
  const search = page.getByRole("searchbox", { name: "Search recommendations" });
  await search.fill("synthetic");
  await search.press("Enter");
  await expect(page).toHaveURL(/q=synthetic/);
  await page.getByRole("button", { name: "Reset Filters" }).click();
  await expect(search).toHaveValue("");
  await page.reload();
  await expect(page.getByRole("heading", { name: "Recommendations", exact: true })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", originalTheme === "dark" ? "light" : "dark");
  expect(await page.evaluate(async () => (await navigator.serviceWorker.getRegistrations()).length)).toBe(0);
  expect(errors).toEqual([]);
});
