// End to end: the first run on a fresh hub. A hub with no password shows
// only its claim step: claim it with the setup code from its log, then go
// through every step of the setup wizard in order. Keep a name and time zone, check the connection, set up this device, add a first
// to-do, pair a real bridge and switch on one of its repos, then finish. An
// agent on the paired machine then adds a to-do with a date, read in the
// hub's time zone rather than the machine's.
//
// Simulated: the browser's push service, which a headless browser has no
// account for. The page reports notifications allowed and hands the app a
// subscription on a known push host with keys no device holds, so the hub
// stores it, fails to encrypt for it before sending anything, and still
// sends the test reminder to the ntfy capture server.

import { test, expect, type Page } from "@playwright/test";
import { hasBd, NTFY_TOPIC, World } from "./world.ts";

const BD_MISSING = "bd is not on PATH: install beads 1.3.x to run the end-to-end suite (CI sets E2E_REQUIRE_BD=1 and installs it)";
const PASSWORD = "water the plants daily";
const DEVICE_ZONE = "Europe/Lisbon";
const HUB_ZONE = "Pacific/Auckland";
const FAKE_ENDPOINT = "https://fcm.googleapis.com/fcm/send/e2e-first-run";
const SETUP_CODE = /[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}/;

if (!process.env.E2E_REQUIRE_BD) test.skip(!hasBd(), BD_MISSING);

let world: World | undefined;
let failed = false;

test.beforeAll(async () => {
  if (process.env.E2E_REQUIRE_BD && !hasBd()) throw new Error(BD_MISSING);
  world = await World.create({ noPassword: true });
  await world.addMachine("laptop", [
    { name: "chores", prefix: "ch", optedIn: false, human: ["Fix the porch light"], other: ["Tidy the scripts folder"] },
    { name: "garden", prefix: "ga", optedIn: false, human: ["Prune the roses"] },
  ]);
});

test.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) failed = true;
});

test.afterAll(async () => {
  if (failed && world) console.log(world.logs());
  await world?.dispose();
  world = undefined;
});

test.use({ timezoneId: DEVICE_ZONE, permissions: ["clipboard-read", "clipboard-write"] });

/** Stands in for the browser's notification permission and push service (see the top of this file). */
async function simulatePush(page: Page) {
  await page.addInitScript((endpoint) => {
    class FakeNotification {
      static permission = "default";
      static async requestPermission() {
        FakeNotification.permission = "granted";
        return FakeNotification.permission;
      }
    }
    Object.defineProperty(window, "Notification", { value: FakeNotification, configurable: true, writable: true });
    Object.defineProperty(window, "PushManager", { value: class {}, configurable: true, writable: true });
    let current: object | null = null;
    const sub = {
      toJSON: () => ({ endpoint, keys: { p256dh: "BExampleKeyForTheE2eSuiteOnly", auth: "ExampleAuthKey" } }),
      unsubscribe: async () => {
        current = null;
        return true;
      },
    };
    const reg = { pushManager: { getSubscription: async () => current, subscribe: async () => (current = sub) } };
    Object.defineProperty(navigator, "serviceWorker", {
      value: { register: async () => reg, ready: Promise.resolve(reg) },
      configurable: true,
    });
  }, FAKE_ENDPOINT);
}

test("a fresh hub is set up from the first visit to a paired machine", async ({ page }) => {
  test.setTimeout(180_000);
  const w = world!;
  const wiz = page.locator("#wizard");
  const heading = wiz.locator("#wiz-h");
  const next = wiz.locator("[data-act=wizard-next]");
  const step = (id: string) => wiz.locator(`[data-step="${id}"]`);
  const go = async (from: string) => {
    await next.click();
    await expect(heading).not.toHaveText(from);
  };
  await simulatePush(page);

  await test.step("claim the hub with the setup code from its log", async () => {
    await page.goto(w.appURL);
    // A hub with no password shows the claim step and nothing else.
    await expect(page.getByLabel("Setup code")).toBeVisible();
    await expect(page.locator("#list")).toHaveCount(0);
    const code = SETUP_CODE.exec(w.hub.output)?.[0];
    expect(code, "the hub logs a setup code").toBeTruthy();
    await page.getByLabel("Setup code").fill(code!);
    await page.getByLabel("New password", { exact: true }).fill(PASSWORD);
    await page.getByLabel("Repeat the password").fill(PASSWORD);
    await page.getByRole("button", { name: "Set password" }).click();
    // The claim step gives way to the app at once.
    await expect(page.getByLabel("Setup code")).toHaveCount(0);
    expect(await w.hubFile("setup-code")).toBeNull();
    // The hub now wants a session: a request without one is refused.
    expect((await fetch(`${w.hubURL}/api/state`)).status).toBe(401);
  });

  await test.step("welcome lists the whole run", async () => {
    await expect(heading).toHaveText("Welcome");
    for (const title of ["Connection check", "Name and time zone", "Reminders", "First to-do", "Agent machines"]) {
      await expect(wiz.locator(".wiz-list")).toContainText(title);
    }
    await go("Welcome");
  });

  await test.step("connection check passes", async () => {
    await expect(heading).toHaveText("Connection check");
    const checks = wiz.getByRole("list", { name: "Connection checks" });
    for (const title of ["Address", "Secure connection", "Live updates", "Clocks", "Reminder keys", "ntfy", "Public DNS"]) {
      await expect(checks.locator(".conn-pass").filter({ has: page.getByText(title, { exact: true }) })).toBeVisible();
    }
    await expect(checks.locator(".conn-fail")).toHaveCount(0);
    await go("Connection check");
    // The hub is claimed already; a step about its password only reports it.
    if ((await heading.textContent()) === "Secure the hub") await go("Secure the hub");
  });

  await test.step("restore is offered on the fresh hub, and skipped", async () => {
    await expect(heading).toHaveText("Restore from an export");
    await wiz.getByRole("button", { name: "Skip" }).click();
  });

  await test.step("keep a name and a time zone", async () => {
    await expect(heading).toHaveText("Name and time zone");
    const profile = step("profile");
    await expect(profile.getByLabel("Name")).toHaveValue("alex");
    await expect(profile.getByLabel("Time zone")).toHaveValue(DEVICE_ZONE);
    await profile.getByLabel("Name").fill("Sam");
    await profile.getByLabel("Time zone").fill(HUB_ZONE);
    await go("Name and time zone");
    const settings = await page.request.get(`${w.appURL}/api/settings`);
    expect(await settings.json()).toEqual({ name: "Sam", timeZone: HUB_ZONE, reminderLead: 0 });
  });

  await test.step("install, then turn on reminders and get a test", async () => {
    await expect(heading).toHaveText("Install the app");
    await expect(step("install")).toContainText("Installing is optional");
    await go("Install the app");

    await expect(heading).toHaveText("Reminders");
    await step("reminders").getByRole("button", { name: "Turn on reminders" }).click();
    const status = step("reminders").getByRole("status");
    // The made-up keys cannot be encrypted for, so only ntfy gets the test.
    await expect(status).toContainText("could not send the test reminder to this device");
    await expect(status).toContainText("A test reminder was also sent to ntfy.");
    const ntfy = w.ntfy.requests.find((r) => r.headers.title === "Test reminder");
    expect(ntfy?.url).toBe(`/${NTFY_TOPIC}`);
    await go("Reminders");

    await expect(heading).toHaveText("Appearance");
    await step("appearance").getByRole("radio", { name: /^Light/ }).check();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await go("Appearance");
  });

  await test.step("add a first to-do with quick add", async () => {
    await expect(heading).toHaveText("First to-do");
    await page.getByRole("button", { name: "Use the example" }).click();
    await expect(page.locator("#ft-read")).toContainText("Due Tomorrow 18:00");
    await page.getByRole("button", { name: "Add to-do" }).click();
    await expect(page.locator(".wiz-added")).toContainText('Added "Water the plants" to personal.');
    await go("First to-do");
  });

  let token = "";
  await test.step("pair a machine and switch on a repo", async () => {
    await expect(heading).toHaveText("Agent machines");
    await wiz.getByRole("button", { name: "Yes, add a machine" }).click();
    await page.getByLabel("Machine name").fill("laptop");
    await wiz.getByRole("button", { name: "Add machine" }).click();
    const cmd = page.locator("#agents-pair-cmd");
    await expect(cmd).toContainText(`meat-mode bridge pair ${w.appURL} `);
    const [, , , url, t] = (await cmd.textContent())!.trim().split(/\s+/);
    token = t;
    await expect(wiz.getByRole("status")).toHaveText("Waiting for laptop to connect…");
    expect(await w.pair("laptop", url, token)).toContain('as bridge "laptop"');
    w.startBridge("laptop");
    await expect(wiz.getByRole("status")).toHaveText("Connected. laptop is talking to this hub.");
    await go("Agent machines");

    await expect(heading).toHaveText("Choose repos");
    const chores = wiz.getByRole("switch", { name: /^Sync chores / });
    await expect(chores).toHaveAttribute("aria-checked", "false");
    await chores.click();
    await expect(chores).toHaveAttribute("aria-checked", "true");
    await go("Choose repos");

    await expect(heading).toHaveText("Tell your agents");
    const text = page.locator("#wiz-agents-text");
    await expect(text).toContainText("## Tasks for Sam");
    await expect(text).toContainText('paired with it as "laptop"');
    await wiz.getByRole("button", { name: "Copy instructions" }).click();
    const copied = await page.evaluate(() => navigator.clipboard.readText());
    expect(copied).toContain("meat-mode add");
    expect(copied).not.toContain(token);
    await go("Tell your agents");
  });

  await test.step("finish lists what is set up", async () => {
    await expect(heading).toHaveText("All set");
    const outcome = (title: string) => wiz.locator(".wiz-summary li", { hasText: title });
    for (const title of ["Connection check", "Name and time zone", "Reminders", "First to-do", "Agent machines", "Choose repos"]) {
      await expect(outcome(title)).toContainText("Set up");
    }
    await next.click();
    await expect(page.locator("#view-title")).toHaveText("Today");
    const setup = await page.request.get(`${w.appURL}/api/setup`);
    expect(await setup.json()).toMatchObject({ status: "done", password: "app", user: "Sam", timeZone: HUB_ZONE });
  });

  await test.step("the list fills from the app, the repo and an agent", async () => {
    // An agent on the machine, which runs in UTC, adds a to-do for a date: it is due at 9:00 in the hub's zone.
    await w.run("laptop", ["add", "Renew the library card", "--due", "2030-03-02"], { TZ: "UTC" });

    await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Everything/ }).click();
    const title = (t: string) => page.locator("#list .row-title", { hasText: t });
    await expect(title("Water the plants")).toBeVisible();
    await expect(title("Fix the porch light")).toBeVisible();
    await expect(title("Renew the library card")).toBeVisible();
    await expect(title("Tidy the scripts folder")).toHaveCount(0);
    await expect(title("Prune the roses")).toHaveCount(0);

    const state = (await (await page.request.get(`${w.appURL}/api/state`)).json()) as { items: { title: string; dueAt?: string }[] };
    const card = state.items.find((i) => i.title === "Renew the library card");
    // 9:00 on 2 March in Auckland, which is on daylight time (UTC+13) then.
    expect(card?.dueAt).toBe("2030-03-01T20:00:00Z");
  });
});
