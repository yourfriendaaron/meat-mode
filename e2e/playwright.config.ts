import { defineConfig } from "@playwright/test";

// End-to-end suite: a real hub (binary, or the image with E2E_IMAGE), two real
// bridges and real bd repos in temp directories. The tests share one world and
// run in order, so there is one worker. The hub's URL is set per test from the
// world fixture, not here.
export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.ts",
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 20_000 },
  reporter: "list",
  use: {
    browserName: "chromium",
    viewport: { width: 1280, height: 800 },
    timezoneId: "UTC",
    locale: "en-US",
    trace: "retain-on-failure",
  },
});
