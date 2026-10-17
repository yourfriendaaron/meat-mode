import { test, expect, axe, wizard, openWizardAt, openApp } from "./setup.ts";
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

async function fillClaim(page: Page, code: string, password: string, repeat = password) {
  const s = step(page);
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

  test("the Secure step sets the password with the setup code and logs this device in", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    const w = wizard(page);
    const s = step(page);
    await expect(w.heading).toHaveText("Secure the hub");
    await expect(s).toContainText("anyone who can reach its address");
    await expect(s).toContainText("docker logs");
    await expect(s.locator("code", { hasText: "setup-code" })).toBeVisible();
    await expect(s.getByLabel("Setup code")).toHaveAttribute("autocomplete", "one-time-code");
    await expect(s.getByLabel("New password", { exact: true })).toHaveAttribute("autocomplete", "new-password");
    await axe(page);
    await lightTheme(page);
    await axe(page);

    const set = s.getByRole("button", { name: "Set password" });
    await fillClaim(page, "ABCD-EFGH-JKLM", "short");
    await set.click();
    await expect(s.getByRole("alert")).toHaveText("The password must be at least 8 characters.");
    await expect(s.getByLabel("Setup code")).toHaveValue("ABCD-EFGH-JKLM");
    await expect(s.getByLabel("New password", { exact: true })).toHaveAttribute("aria-describedby", "wiz-pw-error");

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
    await expect(s.getByRole("status")).toHaveText("The password is set, and this device is logged in.");
    await expect(s.getByRole("status")).toBeFocused();
    await expect(s.locator("form")).toHaveCount(0);
    expect(hub.setup.password).toBe("app");
    expect(hub.password).toBe(NEW_PASSWORD);
    await axe(page);

    await w.next.click();
    await expect(w.heading).toHaveText("All set");
    await expect(w.region.locator(".wiz-summary li", { hasText: "Secure the hub" })).toContainText("Set up");

    // Logged in: Settings has Log out and the change form.
    await w.region.getByRole("button", { name: "Secure the hub in Settings" }).click();
    await expect(page.locator("#s-password")).toBeFocused();
    await expect(page.getByRole("button", { name: "Log out" })).toBeVisible();
    await expect(page.locator('[data-section="password"]').getByLabel("Current password")).toBeVisible();
  });

  test("Continue sets the password when the form is filled, and stays when it is empty", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    const w = wizard(page);
    await w.next.click();
    await expect(page.locator("#toast")).toContainText("Set a password, choose no password, or skip this step.");
    await expect(w.heading).toHaveText("Secure the hub");

    await fillClaim(page, "ABCD-EFGH-JKLM", NEW_PASSWORD);
    await w.next.click();
    await expect(w.heading).toHaveText("All set");
    expect(hub.setup.password).toBe("app");
    expect(hub.requests).toContain("POST /api/setup/claim");
  });

  test("a wrong code on Continue keeps the step and says why", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    const w = wizard(page);
    hub.claimStatus = 429;
    await fillClaim(page, "ABCD-EFGH-JKLM", NEW_PASSWORD);
    await w.next.click();
    await expect(step(page).getByRole("alert")).toHaveText("Too many wrong codes. Wait a minute and try again.");
    await expect(w.heading).toHaveText("Secure the hub");
  });

  test("no password, Tailscale only, is recorded and counts as set up", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    const w = wizard(page);
    const s = step(page);
    await s.getByRole("button", { name: "No password, Tailscale only" }).click();
    await expect(s).toContainText("You chose no password");
    await expect(s.getByRole("button", { name: "No password, Tailscale only" })).toHaveCount(0);
    expect(hub.setup.tailscaleOnly).toBe(true);
    await axe(page);

    await w.next.click();
    await expect(w.heading).toHaveText("All set");
    await expect(w.region.locator(".wiz-summary li", { hasText: "Secure the hub" })).toContainText("Set up");
  });

  test("a password from the environment: the step and Settings say so and offer no form", async ({ page, hub }) => {
    hub.setup.password = "env";
    await openWizardAt(page, hub, "secure");
    const s = step(page);
    await expect(s).toContainText("set in its environment");
    await expect(s.locator("code", { hasText: "MEAT_MODE_PASSWORD_HASH" }).first()).toBeVisible();
    await expect(s.locator("form")).toHaveCount(0);
    await expect(s.getByRole("button", { name: "No password, Tailscale only" })).toHaveCount(0);
    await axe(page);

    const section = await openSettings(page);
    await expect(section).toContainText("set in the hub's environment");
    await expect(section.locator("form")).toHaveCount(0);
  });

  test("Settings changes the password and says other devices are logged out", async ({ page, hub }) => {
    hub.login = true;
    hub.authenticated = true;
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

  test("Settings claims a hub with no password", async ({ page, hub }) => {
    await openApp(page);
    const section = await openSettings(page);
    await expect(section).toContainText("This hub has no password.");
    await expect(page.getByRole("button", { name: "Log out" })).toHaveCount(0);
    await section.getByLabel("Setup code").fill("ABCD-EFGH-JKLM");
    await section.getByLabel("New password", { exact: true }).fill(NEW_PASSWORD);
    await section.getByLabel("Repeat the password").fill(NEW_PASSWORD);
    await section.getByRole("button", { name: "Set password" }).click();
    await expect(section.getByLabel("Current password")).toBeVisible();
    await expect(page.getByRole("button", { name: "Log out" })).toBeVisible();
    expect(hub.setup.password).toBe("app");
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test("the Secure step fits the screen", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    await expect(step(page).getByLabel("Setup code")).toBeVisible();
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await axe(page);
  });
});
