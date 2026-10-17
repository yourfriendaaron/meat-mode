// End to end: claiming a new hub in the browser with the setup code it
// writes at start, then changing the password in Settings. A real hub with no
// password in its environment, and a second one with a password there, which
// refuses claiming. No bridges or bd. The tests in each block share one world
// and run in order.

import { test as base, expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { stat } from "node:fs/promises";
import { join } from "node:path";
import { World } from "./world.ts";

const FIRST = "water the plants";
const SECOND = "sweep the porch";
const ENV_PASSWORD = "renew the library card";

base.describe.configure({ mode: "serial" });

let world: World | undefined;
let failed = false;
const contexts: BrowserContext[] = [];

const test = base.extend<{ w: World }>({
  w: async ({}, use) => {
    if (!world) throw new Error("world not set up");
    await use(world);
  },
});

base.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) failed = true;
});

async function teardown() {
  if (failed && world) console.log(world.logs());
  for (const c of contexts.splice(0)) await c.close();
  await world?.dispose();
  world = undefined;
  failed = false;
}

async function newPage(browser: Browser): Promise<Page> {
  const ctx = await browser.newContext();
  contexts.push(ctx);
  return ctx.newPage();
}

const wizard = (page: Page) => page.locator("#wizard");
const heading = (page: Page) => page.locator("#wiz-h");

/** Skips the steps on the way to the one titled title, whichever steps come before it. */
async function skipUntil(page: Page, title: string) {
  for (let i = 0; i < 30; i++) {
    const h = (await heading(page).textContent()) ?? "";
    if (h === title) return;
    await wizard(page).getByRole("button", { name: "Skip" }).click();
    await expect(heading(page)).not.toHaveText(h);
  }
  throw new Error(`setup did not reach ${title}`);
}

async function logIn(page: Page, url: string, password: string) {
  await page.goto(url);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page.locator("#view-title")).toBeVisible();
}

base.describe("a hub with no password", () => {
  let alex: Page;
  let sam: Page;

  base.beforeAll(async ({ browser }) => {
    world = await World.create();
    alex = await newPage(browser);
    sam = await newPage(browser);
  });
  base.afterAll(teardown);

  test("writes a one-time setup code to its log and data folder", async ({ w }) => {
    const code = (await w.hubFile("setup-code"))?.trim() ?? "";
    expect(code).toMatch(/^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$/);
    expect(w.hub.output).toContain(code);
    if (!process.env.E2E_IMAGE) {
      const st = await stat(join(w.tmp, "hub-data", "setup-code"));
      expect(st.mode & 0o777).toBe(0o600);
    }
    expect(await w.api("/api/setup")).toMatchObject({ status: "new", password: "none" });
  });

  test("the wizard claims the hub with the code and logs this browser in", async ({ w }) => {
    const code = (await w.hubFile("setup-code"))!.trim();
    await alex.goto(w.appURL);
    await expect(heading(alex)).toHaveText("Welcome");
    await wizard(alex).getByRole("button", { name: "Start" }).click();
    await expect(heading(alex)).not.toHaveText("Welcome");
    await skipUntil(alex, "Secure the hub");

    const step = wizard(alex).locator('[data-step="secure"]');
    const fill = async (c: string) => {
      await step.getByLabel("Setup code").fill(c);
      await step.getByLabel("New password", { exact: true }).fill(FIRST);
      await step.getByLabel("Repeat the password").fill(FIRST);
      await step.getByRole("button", { name: "Set password" }).click();
    };
    await fill("ZZZZ-ZZZZ-ZZZZ");
    await expect(step.getByRole("alert")).toContainText("wrong, expired or already used");

    await fill(code.toLowerCase());
    await expect(step.getByRole("status")).toHaveText("The password is set, and this device is logged in.");
    expect(await w.hubFile("setup-code")).toBeNull();
    expect(await w.api("/api/session")).toEqual({ login: true, authenticated: false });

    await wizard(alex).locator("[data-act=wizard-next]").click();
    await expect(heading(alex)).not.toHaveText("Secure the hub");
    await skipUntil(alex, "All set");
    await expect(wizard(alex).locator(".wiz-summary li", { hasText: "Secure the hub" })).toContainText("Set up");
    await wizard(alex).locator("[data-act=wizard-next]").click();
    await expect(alex.locator("#view-title")).toHaveText("Today");

    // The session holds across a reload.
    await alex.reload();
    await expect(alex.locator("#view-title")).toHaveText("Today");
    await expect(alex.getByLabel("Password")).toHaveCount(0);
  });

  test("claiming works only once", async ({ w }) => {
    const r = await fetch(`${w.hubURL}/api/setup/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Origin: w.hubURL },
      body: JSON.stringify({ code: "ZZZZ-ZZZZ-ZZZZ", password: "anything at all" }),
    });
    expect(r.status).toBe(409);
  });

  test("another browser needs the new password", async ({ w }) => {
    await logIn(sam, w.appURL, FIRST);
    await expect(sam.locator("#view-title")).toHaveText("Today");
  });

  test("changing the password in Settings logs out every other device", async ({ w }) => {
    await alex.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const section = alex.locator('[data-section="password"]');
    await section.getByLabel("Current password").fill(FIRST);
    await section.getByLabel("New password", { exact: true }).fill(SECOND);
    await section.getByLabel("Repeat the new password").fill(SECOND);
    await section.getByRole("button", { name: "Change password" }).click();
    await expect(section.getByRole("status")).toHaveText("The password is changed. Every other device is logged out.");

    // This browser stays in; the other one is sent to the login screen.
    await alex.reload();
    await expect(alex.locator("#view-title")).toBeVisible();
    await expect(alex.getByLabel("Password")).toHaveCount(0);
    await sam.reload();
    await expect(sam.getByRole("button", { name: "Log in" })).toBeVisible();
    await sam.getByLabel("Password").fill(FIRST);
    await sam.getByRole("button", { name: "Log in" }).click();
    await expect(sam.getByRole("alert")).toHaveText("Wrong password.");
    await logIn(sam, w.appURL, SECOND);
  });

  test("a restart makes no new code once the hub has a password", async ({ w }) => {
    await w.restartHub();
    expect(await w.hubFile("setup-code")).toBeNull();
    await alex.reload();
    await expect(alex.locator("#view-title")).toBeVisible();
    await expect(alex.getByLabel("Password")).toHaveCount(0);
  });
});

base.describe("a hub with a password in its environment", () => {
  let alex: Page;

  base.beforeAll(async ({ browser }) => {
    world = await World.create({ password: ENV_PASSWORD });
    alex = await newPage(browser);
  });
  base.afterAll(teardown);

  test("makes no setup code and refuses claiming", async ({ w }) => {
    expect(await w.hubFile("setup-code")).toBeNull();
    const r = await fetch(`${w.hubURL}/api/setup/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Origin: w.hubURL },
      body: JSON.stringify({ code: "ZZZZ-ZZZZ-ZZZZ", password: "anything at all" }),
    });
    expect(r.status).toBe(409);
    expect(((await r.json()) as { error: string }).error).toContain("environment");
  });

  test("the wizard and Settings say the environment sets the password", async ({ w }) => {
    await logIn(alex, w.appURL, ENV_PASSWORD);
    await expect(heading(alex)).toHaveText("Welcome");
    await wizard(alex).getByRole("button", { name: "Start" }).click();
    await expect(heading(alex)).not.toHaveText("Welcome");
    await skipUntil(alex, "Secure the hub");
    const step = wizard(alex).locator('[data-step="secure"]');
    await expect(step).toContainText("set in its environment");
    await expect(step.locator("form")).toHaveCount(0);

    await alex.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await expect(alex.locator('[data-section="password"]')).toContainText("set in the hub's environment");
  });
});
