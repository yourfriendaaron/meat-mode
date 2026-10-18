import { defineConfig } from "@playwright/test";

// The screenshot spec. It runs only inside the Playwright container
// (scripts/visual.sh, `pnpm test:visual`), whose fonts and browser match CI's,
// so the baselines in test/visual/__screenshots__ hold on every machine.
// The container has its own network, so the port is fixed: it shows up in
// the pages under test.
const PORT = 4173;

export default defineConfig({
  testDir: "test/visual",
  snapshotPathTemplate: "{testDir}/__screenshots__/{arg}{ext}",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [["list"]],
  expect: {
    toHaveScreenshot: { animations: "disabled", caret: "hide", scale: "css" },
  },
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    browserName: "chromium",
    timezoneId: "UTC",
    locale: "en-US",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "node test/serve.mjs",
    url: `http://127.0.0.1:${PORT}/`,
    env: { PORT: String(PORT) },
    reuseExistingServer: false,
    stdout: "ignore",
  },
});
