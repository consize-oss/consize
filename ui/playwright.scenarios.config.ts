import {
  defineConfig,
  devices,
} from "../tools/ci/node_modules/@playwright/test/index.js";
export default defineConfig({
  testDir: "./tests/scenarios",
  forbidOnly: true,
  retries: 0,
  workers: 1,
  timeout: 60_000,
  reporter: [
    ["list"],
    ["html", { open: "never", outputFolder: "playwright-scenarios-report" }],
  ],
  use: {
    baseURL: "http://127.0.0.1:3032",
    serviceWorkers: "allow",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "npm run dev -- --hostname 127.0.0.1 --port 3032",
    url: "http://127.0.0.1:3032",
    env: {
      NEXT_PUBLIC_USE_MOCKS: "true",
      NEXT_PUBLIC_CONSIZE_API_BASE_URL: "http://127.0.0.1:18089",
    },
    reuseExistingServer: false,
    timeout: 120_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 5000 },
  },
});
