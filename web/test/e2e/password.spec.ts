import { test, expect, axe, row, wizard, openWizardAt, openApp } from "./setup.ts";
import type { Page } from "@playwright/test";

const NEW_PASSWORD = "water the plants";

async function lightTheme(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem("meat-mode.theme", "light");
    document.documentElement.dataset.theme = "light";
  });
}

function step(page: Page) {
  return wizard(page).region.locator('[data-step="secure"]');
}

function claimScreen(page: Page) {
  return page.locator(".claim");
}

async function fillClaim(page: Page, code: string, password: string, repeat = password) {
  const s = claimScreen(page);
  await s.getByLabel("Setup code").fill(code);
  await s.getByLabel("New password", { exact: true }).fill(password);
  await s.getByLabel("Repeat the password").fill(repeat);
}

async function openSettings(page: Page) {
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  return page.locator('[data-section="password"]');
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("a hub with no password shows only the claim step, and setting the password opens the app", async ({ page, hub }) => {
    hub.login = false;
    await page.goto("/");
    const s = claimScreen(page);
    await expect(s.getByRole("heading", { name: "Meat Mode" })).toBeVisible();
    await expect(s).toContainText("This hub has no password yet.");
    await expect(s).toContainText("docker logs");
    await expect(s.locator("code", { hasText: "setup-code" })).toBeVisible();
    await expect(s.getByLabel("Setup code")).toBeFocused();
    await expect(s.getByLabel("Setup code")).toHaveAttribute("autocomplete", "one-time-code");
    await expect(s.getByLabel("New password", { exact: true })).toHaveAttribute("autocomplete", "new-password");
    // Nothing but the claim step: no list, no navigation, no other request.
    await expect(page.locator("#list")).toHaveCount(0);
    await expect(page.locator(".sidebar")).toHaveCount(0);
    await expect(page.getByRole("button", { name: /no password/i })).toHaveCount(0);
    expect(hub.requests).toEqual(["GET /api/session"]);
    await axe(page);
    await lightTheme(page);
    await axe(page);

    const set = s.getByRole("button", { name: "Set password" });
    await fillClaim(page, "ABCD-EFGH-JKLM", "short");
    await set.click();
    await expect(s.getByRole("alert")).toHaveText("The password must be at least 8 characters.");
    await expect(s.getByLabel("Setup code")).toHaveValue("ABCD-EFGH-JKLM");
    await expect(s.getByLabel("New password", { exact: true })).toHaveAttribute("aria-describedby", "claim-pw-error");

    await fillClaim(page, "ABCD-EFGH-JKLM", NEW_PASSWORD, "water the plant");
    await set.click();
    await expect(s.getByRole("alert")).toHaveText("The two passwords do not match.");

    await fillClaim(page, "ZZZZ-ZZZZ-ZZZZ", NEW_PASSWORD);
    await set.click();
    await expect(s.getByRole("alert")).toHaveText("That setup code is wrong, expired or already used. Restart the hub for a new one.");
    await expect(s.getByRole("alert")).toBeFocused();
    expect(hub.setup.password).toBe("none");
    await axe(page);

    // Case, spaces and dashes do not matter.
    await fillClaim(page, "abcd efgh jklm", NEW_PASSWORD);
    await set.click();
    await expect(row(page, "Water the plants")).toBeVisible();
    await expect(claimScreen(page)).toHaveCount(0);
    expect(hub.setup.password).toBe("app");
    expect(hub.password).toBe(NEW_PASSWORD);

    // Logged in: Settings has Log out and the change form.
    const section = await openSettings(page);
    await expect(page.getByRole("button", { name: "Log out" })).toBeVisible();
    await expect(section.getByLabel("Current password")).toBeVisible();
  });

  test("too many wrong codes says to wait", async ({ page, hub }) => {
    hub.login = false;
    hub.claimStatus = 429;
    await page.goto("/");
    await fillClaim(page, "ABCD-EFGH-JKLM", NEW_PASSWORD);
    await claimScreen(page).getByRole("button", { name: "Set password" }).click();
    await expect(claimScreen(page).getByRole("alert")).toHaveText("Too many wrong codes. Wait a minute and try again.");
  });

  test("a fresh hub's claim opens the setup wizard", async ({ page, hub }) => {
    hub.fresh();
    hub.login = false;
    await page.goto("/");
    await fillClaim(page, "ABCD-EFGH-JKLM", NEW_PASSWORD);
    await claimScreen(page).getByRole("button", { name: "Set password" }).click();
    await expect(wizard(page).heading).toHaveText("Welcome");
    expect(hub.setup.password).toBe("app");
  });

  test("a hub that loses its password while open shows the claim step", async ({ page, hub }) => {
    await openApp(page);
    hub.login = false;
    await row(page, "Water the plants").locator(".check").click();
    await expect(claimScreen(page).getByLabel("Setup code")).toBeVisible();
    await expect(page.locator("#list")).toHaveCount(0);
  });

  test("the Secure step says the hub has a password and offers no other choice", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    hub.setup.password = "app";
    await page.reload();
    const s = step(page);
    await expect(wizard(page).heading).toHaveText("Secure the hub");
    await expect(s).toContainText("This hub has a password.");
    await expect(s.locator("form")).toHaveCount(0);
    await expect(s.getByRole("button", { name: /no password/i })).toHaveCount(0);
    await axe(page);
  });

  test("a password from the environment: the step and Settings say so and offer no form", async ({ page, hub }) => {
    hub.setup.password = "env";
    await openWizardAt(page, hub, "secure");
    const s = step(page);
    await expect(s).toContainText("set in its environment");
    await expect(s.locator("code", { hasText: "MEAT_MODE_PASSWORD_HASH" }).first()).toBeVisible();
    await expect(s.locator("form")).toHaveCount(0);
    await axe(page);

    const section = await openSettings(page);
    await expect(section).toContainText("set in the hub's environment");
    await expect(section.locator("form")).toHaveCount(0);
  });

  test("Settings changes the password and says other devices are logged out", async ({ page, hub }) => {
    hub.setup.password = "app";
    await openApp(page);
    const section = await openSettings(page);
    await expect(page.locator("#s-password")).toHaveText("Password");
    await expect(section).toContainText("Every other device is logged out");
    await axe(page);

    const change = section.getByRole("button", { name: "Change password" });
    await section.getByLabel("Current password").fill("not it");
    await section.getByLabel("New password", { exact: true }).fill(NEW_PASSWORD);
    await section.getByLabel("Repeat the new password").fill(NEW_PASSWORD);
    await change.click();
    await expect(section.getByRole("alert")).toHaveText("The current password is wrong.");
    expect(hub.password).not.toBe(NEW_PASSWORD);

    await section.getByLabel("Current password").fill(hub.password);
    await change.click();
    await expect(section.getByRole("status")).toHaveText("The password is changed. Every other device is logged out.");
    expect(hub.password).toBe(NEW_PASSWORD);
    await expect(section.getByLabel("Current password")).toHaveValue("");
    await expect(section.getByLabel("New password", { exact: true })).toHaveValue("");
    await axe(page);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test("the claim step fits the screen", async ({ page, hub }) => {
    hub.login = false;
    await page.goto("/");
    await expect(claimScreen(page).getByLabel("Setup code")).toBeVisible();
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await axe(page);
  });
});
