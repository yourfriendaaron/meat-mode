// End to end through the setup wizard's agent steps: a fresh real hub, one
// machine with real bd repos, and `meat-mode bridge pair` run with the
// command the wizard shows. The wizard opens straight at its agent step, so
// the test does not depend on the steps before it.

import { test, expect } from "@playwright/test";
import { hasBd, USER, World } from "./world.ts";

const BD_MISSING = "bd is not on PATH: install beads 1.3.x to run the end-to-end suite (CI sets E2E_REQUIRE_BD=1 and installs it)";

if (!process.env.E2E_REQUIRE_BD) test.skip(!hasBd(), BD_MISSING);

let world: World | undefined;
let failed = false;

test.beforeAll(async () => {
  if (process.env.E2E_REQUIRE_BD && !hasBd()) throw new Error(BD_MISSING);
  world = await World.create();
  await world.addMachine("laptop", [
    { name: "chores", prefix: "ch", optedIn: false, human: ["Water the plants"], other: ["Tidy the scripts folder"] },
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

test.use({ permissions: ["clipboard-read", "clipboard-write"] });

test("pairs a machine from the wizard, chooses a repo and copies the instructions", async ({ page }) => {
  const w = world!;
  // Resume a first run at the agent step, as if the user had got there and reloaded.
  await page.addInitScript(() => {
    if (sessionStorage.getItem("seeded-setup")) return;
    sessionStorage.setItem("seeded-setup", "1");
    localStorage.setItem("meat-mode.setup", JSON.stringify({ mode: "first", step: "agents", done: [], skipped: [] }));
  });
  await page.goto(w.hubURL + "/");
  const wiz = page.locator("#wizard");
  const heading = wiz.locator("#wiz-h");
  const next = wiz.locator("[data-act=wizard-next]");
  await expect(heading).toHaveText("Agent machines");

  await wiz.getByRole("button", { name: "Yes, add a machine" }).click();
  await page.getByLabel("Machine name").fill("laptop");
  await wiz.getByRole("button", { name: "Add machine" }).click();
  const cmd = page.locator("#agents-pair-cmd");
  await expect(cmd).toContainText(`meat-mode bridge pair ${w.hubURL} `);
  const [, , , url, token] = (await cmd.textContent())!.trim().split(/\s+/);
  await expect(wiz.getByRole("status")).toHaveText("Waiting for laptop to connect…");

  expect(await w.pair("laptop", url, token)).toContain('as bridge "laptop"');
  w.startBridge("laptop");
  await expect(wiz.getByRole("status")).toHaveText("Connected. laptop is talking to this hub.");

  await next.click();
  await expect(heading).toHaveText("Choose repos");
  const chores = wiz.getByRole("switch", { name: /^Sync chores / });
  await expect(chores).toHaveAttribute("aria-checked", "false");
  await expect(wiz.getByRole("switch", { name: /^Sync garden / })).toBeVisible();
  await chores.click();
  await expect(chores).toHaveAttribute("aria-checked", "true");
  await expect(wiz.getByText("Waiting for laptop")).toHaveCount(0);

  await next.click();
  await expect(heading).toHaveText("Tell your agents");
  const text = page.locator("#wiz-agents-text");
  await expect(text).toContainText(`## Tasks for ${USER}`);
  await expect(text).toContainText('paired with it as "laptop"');
  await wiz.getByRole("button", { name: "Copy instructions" }).click();
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied).toBe(await text.textContent());
  expect(copied).toContain("-l human");
  expect(copied).toContain("meat-mode add");
  expect(copied).not.toContain(token);
  expect(copied).not.toContain(w.hubURL);

  await next.click();
  await expect(heading).toHaveText("All set");
  await next.click();
  await expect(page.locator("#view-title")).toHaveText("Today");
  expect((await w.api<{ status: string }>("/api/setup")).status).toBe("done");

  // The opted-in repo's human bead arrives; its other bead and the other repo's do not.
  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Everything/ }).click();
  const title = (t: string) => page.locator("#list .row-title", { hasText: t });
  await expect(title("Water the plants")).toBeVisible();
  await expect(title("Tidy the scripts folder")).toHaveCount(0);
  await expect(title("Prune the roses")).toHaveCount(0);
});
