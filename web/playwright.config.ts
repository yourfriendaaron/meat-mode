import { defineConfig } from "@playwright/test";

const PORT = Number(process.env.E2E_PORT || 4173);

export default defineConfig({
  testDir: "test/e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "list" : [["list"]],
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    browserName: "chromium",
    timezoneId: "UTC",
    locale: "en-US",
    trace: "retain-on-failure",
  },
  // The server builds the bundle first, so a run never tests a stale one.
  webServer: {
    command: "node scripts/build.mjs && node test/serve.mjs",
    url: `http://127.0.0.1:${PORT}/`,
    env: { PORT: String(PORT) },
    reuseExistingServer: false,
    stdout: "ignore",
  },
});
