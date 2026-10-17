import { test, expect, axe, wizard, openWizardAt } from "./setup.ts";

const check = (page: import("@playwright/test").Page, title: string) =>
  page.locator("#wizard .check", { has: page.locator("strong", { hasText: new RegExp(`^${title}$`) }) });

test.beforeEach(({ hub }) => {
  hub.liveEvents = true;
});

test("a healthy connection passes every check, and Continue goes on", async ({ page, hub }) => {
  await openWizardAt(page, hub, "connection");
  const w = wizard(page);
  await expect(w.heading).toHaveText("Connection check");
  for (const title of ["Address", "Secure connection", "Live updates", "Clocks", "Reminder keys"]) {
    await expect(check(page, title).locator(".check-state")).toHaveText("Passed");
  }
  await expect(check(page, "ntfy").locator(".check-state")).toHaveText("Optional");
  expect(hub.requests).toContain("POST /api/setup/test-event");
  await axe(page);
  await w.next.click();
  await expect(w.heading).not.toHaveText("Connection check");
});

test("an address that is not the public one says where to open the app", async ({ page, hub }) => {
  hub.check.hostMatches = false;
  hub.check.publicUrl = "https://meat-mode.example.com";
  await openWizardAt(page, hub, "connection");
  const c = check(page, "Address");
  await expect(c.locator(".check-state")).toHaveText("Problem");
  await expect(c).toContainText("the hub is set up for https://meat-mode.example.com");
  await expect(c.locator(".check-fix")).toContainText("Open the app at https://meat-mode.example.com");
  await expect(c.locator(".check-fix")).toContainText("MEAT_MODE_PUBLIC_URL");
  await axe(page);
});

test("a page that is not a secure context says to use https", async ({ page, hub }) => {
  await page.addInitScript(() => Object.defineProperty(window, "isSecureContext", { value: false }));
  await openWizardAt(page, hub, "connection");
  const c = check(page, "Secure connection");
  await expect(c.locator(".check-state")).toHaveText("Problem");
  await expect(c.locator(".check-fix")).toContainText("only on https:// addresses");
});

test("live updates that never arrive point at proxy buffering", async ({ page, hub }) => {
  hub.liveEvents = false;
  await openWizardAt(page, hub, "connection");
  const c = check(page, "Live updates");
  await expect(c.locator(".check-state")).toHaveText("Problem", { timeout: 10_000 });
  await expect(c).toContainText("No update arrived within 5 seconds.");
  await expect(c.locator(".check-fix")).toContainText("proxy_buffering off");
  await axe(page);
});

test("a device clock that is off by minutes says so", async ({ page, hub }) => {
  hub.check.time = "2026-03-02T08:50:00Z";
  await openWizardAt(page, hub, "connection");
  const c = check(page, "Clocks");
  await expect(c.locator(".check-state")).toHaveText("Problem");
  await expect(c).toContainText(/clock is about \d+ minutes ahead of the hub's/);
  await expect(c.locator(".check-fix")).toContainText("automatic date and time");
});

test("missing push keys say where the hub keeps them", async ({ page, hub }) => {
  hub.check.pushKeys = false;
  await openWizardAt(page, hub, "connection");
  const c = check(page, "Reminder keys");
  await expect(c.locator(".check-state")).toHaveText("Problem");
  await expect(c.locator(".check-fix")).toContainText("data folder");
});

test("Check again runs the checks once more, and Settings has them too", async ({ page, hub }) => {
  hub.check.pushKeys = false;
  await openWizardAt(page, hub, "connection");
  await expect(check(page, "Reminder keys").locator(".check-state")).toHaveText("Problem");
  hub.check.pushKeys = true;
  await page.locator("#wizard").getByRole("button", { name: "Check again" }).click();
  await expect(check(page, "Reminder keys").locator(".check-state")).toHaveText("Passed");

  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  const section = page.locator("[data-section=connection]");
  await expect(section.getByRole("heading", { name: "Connection" })).toBeVisible();
  await expect(section.locator(".check-state").first()).toHaveText("Passed");
  await section.getByRole("button", { name: "Check again" }).click();
  await expect(section.locator(".check-state").first()).toHaveText("Passed");
  await axe(page);
});

test("a hub that cannot be reached shows the error and offers another try", async ({ page, hub }) => {
  await openWizardAt(page, hub, "connection");
  await expect(check(page, "Address")).toBeVisible();
  await page.route("**/api/setup/status", (r) => r.fulfill({ status: 500, contentType: "application/json", body: '{"error":"the hub hit an internal error"}' }));
  await page.locator("#wizard").getByRole("button", { name: "Check again" }).click();
  await expect(page.locator("#wizard .check-error")).toHaveText("Could not reach the hub: the hub hit an internal error");
});

test("a first check that fails settles on the error instead of retrying", async ({ page, hub }) => {
  let calls = 0;
  await page.route("**/api/setup/status", (r) => {
    calls++;
    return r.fulfill({ status: 500, contentType: "application/json", body: '{"error":"the hub hit an internal error"}' });
  });
  await openWizardAt(page, hub, "connection");
  await expect(page.locator("#wizard .check-error")).toBeVisible();
  await page.waitForTimeout(1000);
  await expect(page.locator("#wizard .check-error")).toBeVisible();
  expect(calls).toBe(1);
});

test.describe("phone, light theme", () => {
  test.use({ viewport: { width: 390, height: 780 } });

  test("every failure fits the screen and keeps its contrast", async ({ page, hub }) => {
    hub.check.hostMatches = false;
    hub.check.pushKeys = false;
    hub.check.time = "2026-03-02T08:50:00Z";
    await openWizardAt(page, hub, "connection");
    await page.evaluate(() => {
      localStorage.setItem("meat-mode.theme", "light");
      document.documentElement.dataset.theme = "light";
    });
    await expect(check(page, "Address").locator(".check-state")).toHaveText("Problem");
    await axe(page);
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
  });
});
