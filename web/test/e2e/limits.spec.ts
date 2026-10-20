import { test, expect, openMenu, axe, openApp } from "./setup.ts";
import type { Page } from "@playwright/test";
import type { HarnessInfo, RunLimits } from "../../src/types.ts";

function limits(over: Partial<RunLimits> = {}): RunLimits {
  return {
    budgetUsd: 2,
    timeoutSeconds: 1800,
    runsPerTask: 10,
    concurrent: 2,
    defaults: { budgetUsd: 2, timeoutSeconds: 1800, runsPerTask: 10, concurrent: 2 },
    max: { budgetUsd: 20, timeoutSeconds: 14400, runsPerTask: 50, concurrent: 5 },
    ...over,
  };
}

const CLAUDE: HarnessInfo = { name: "claude", title: "Claude Code", version: "1.0.0", instructionFile: "CLAUDE.md", budget: "native", budgetUsd: 2, timeoutSeconds: 1800, steer: true };

async function openSettings(page: Page) {
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
}

function machine(page: Page, name: string) {
  return page.locator(".machine", { has: page.locator(".m-name", { hasText: new RegExp(`^${name}$`) }) });
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("changes a machine's limits up to its ceilings, and resets one to the machine's default", async ({ page, hub }) => {
    hub.bridges.push({ name: "desk", createdAt: hub.now() });
    hub.hello("desk", [], { ...CLAUDE, limits: limits() });
    await openSettings(page);
    const desk = machine(page, "desk");
    await expect(desk.locator(".m-limits")).toHaveText("Time limit: 30 minutes a run · Stops at $2 a run · 10 runs per task · 2 at once");

    await desk.getByRole("button", { name: "Change the limits on desk" }).click();
    const form = desk.getByRole("form", { name: "Limits on desk" });
    const budget = form.getByLabel("Spending cap a run, in US dollars");
    await expect(budget).toBeFocused();
    await expect(budget).toHaveValue("2");
    await expect(form.getByLabel("Time limit a run, in minutes")).toHaveValue("30");
    await expect(form.getByLabel("Runs per task", { exact: true })).toHaveValue("10");
    await expect(form.getByLabel("Runs at once", { exact: true })).toHaveValue("2");
    await expect(form).toContainText("Up to $20 · Machine default $2");
    await expect(form).toContainText("Up to 4 hours · Machine default 30 minutes");
    await axe(page);

    // Above the ceiling is refused in the app, naming how to raise it.
    await budget.fill("25");
    await form.getByRole("button", { name: "Save limits" }).click();
    await expect(form.getByRole("alert")).toHaveText(
      "The spending cap can be at most $20 a run on desk. To allow more, set MEAT_MODE_AGENT_MAX_BUDGET_USD on desk and restart its bridge.",
    );
    expect(hub.posted.filter((a) => a.kind === "limits")).toHaveLength(0);
    await form.getByLabel("Time limit a run, in minutes").fill("0");
    await budget.fill("5");
    await form.getByRole("button", { name: "Save limits" }).click();
    await expect(form.getByRole("alert")).toHaveText("The time limit must be a whole number of minutes, at least 1.");

    // Only what changed is sent; the time limit goes as seconds.
    await form.getByLabel("Time limit a run, in minutes").fill("60");
    await form.getByRole("button", { name: "Save limits" }).click();
    await expect(form).toHaveCount(0);
    expect(hub.posted.filter((a) => a.kind === "limits")).toEqual([
      expect.objectContaining({ bridge: "desk", payload: { limits: { budgetUsd: 5, timeoutSeconds: 3600 } } }),
    ]);
    await expect(desk.getByRole("status")).toHaveText("Waiting for desk");
    await expect(desk.locator(".m-limits")).toHaveText("Time limit: 1 hour a run · Stops at $5 a run · 10 runs per task · 2 at once");

    hub.applyLimits("desk");
    await page.reload();
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await expect(desk.getByRole("status")).toHaveCount(0);
    await desk.getByRole("button", { name: "Change the limits on desk" }).click();
    await expect(form.getByLabel("Spending cap a run, in US dollars")).toHaveValue("5");
    // Only a limit the app set goes back to the default.
    await expect(form.getByRole("button", { name: /^Use the machine's default/ })).toHaveCount(2);
    await form.getByRole("button", { name: "Use the machine's default spending cap" }).click();
    expect(hub.posted.filter((a) => a.kind === "limits").at(-1)).toMatchObject({ bridge: "desk", payload: { limits: { reset: ["budgetUsd"] } } });
    await expect(form.getByLabel("Spending cap a run, in US dollars")).toHaveValue("2");
    await expect(form.getByRole("button", { name: /^Use the machine's default/ })).toHaveCount(1);

    // Saving with nothing changed sends nothing.
    const sent = hub.posted.length;
    await form.getByRole("button", { name: "Save limits" }).click();
    await expect(form).toHaveCount(0);
    expect(hub.posted.length).toBe(sent);
  });

  test("a harness with no spending cap has no cap to set, and an older bridge shows its limits only", async ({ page, hub }) => {
    hub.bridges.push({ name: "desk", createdAt: hub.now() }, { name: "attic", createdAt: hub.now() });
    hub.hello("desk", [], { name: "codex", title: "Codex", version: "0.50.0", instructionFile: "AGENTS.md", budget: "none", timeoutSeconds: 1800, limits: limits() });
    hub.hello("attic", [], { ...CLAUDE });
    await openSettings(page);

    const desk = machine(page, "desk");
    await desk.getByRole("button", { name: "Change the limits on desk" }).click();
    const form = desk.getByRole("form", { name: "Limits on desk" });
    await expect(form.getByLabel("Runs per task", { exact: true })).toBeVisible();
    await expect(form.getByLabel("Spending cap a run, in US dollars")).toHaveCount(0);
    await expect(form).toContainText("Codex reports no cost, so no spending cap applies: a run stops at its time limit.");
    await expect(desk.locator(".m-limits")).toContainText("No spending cap: a run stops at its time limit");
    await form.getByLabel("Runs at once", { exact: true }).fill("6");
    await form.getByRole("button", { name: "Save limits" }).click();
    await expect(form.getByRole("alert")).toHaveText(
      "Runs at once can be at most 5 on desk. To allow more, set MEAT_MODE_AGENT_MAX_CONCURRENT on desk and restart its bridge.",
    );

    const attic = machine(page, "attic");
    await expect(attic.locator(".m-limits")).toHaveText("Time limit: 30 minutes a run · Stops at $2 a run");
    await expect(attic).toContainText("Update the bridge on attic to change its agent limits here.");
    await expect(attic.getByRole("button", { name: /Change the limits/ })).toHaveCount(0);
    await axe(page);
  });

  test("the hub's refusal shows in the form", async ({ page, hub }) => {
    hub.bridges.push({ name: "desk", createdAt: hub.now() });
    // The app knows a higher ceiling than the hub now holds, as after the bridge lowered it.
    hub.hello("desk", [], { ...CLAUDE, limits: limits() });
    await openSettings(page);
    const desk = machine(page, "desk");
    await desk.getByRole("button", { name: "Change the limits on desk" }).click();
    hub.bridges.find((b) => b.name === "desk")!.harness!.limits!.max.runsPerTask = 12;
    const form = desk.getByRole("form", { name: "Limits on desk" });
    await form.getByLabel("Runs per task", { exact: true }).fill("20");
    await form.getByRole("button", { name: "Save limits" }).click();
    await expect(form.getByRole("alert")).toHaveText("Runs per task can be at most 12 on desk.");
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the limits form fits the screen", async ({ page, hub }) => {
    hub.bridges.push({ name: "desk", createdAt: hub.now() });
    hub.hello("desk", [], { ...CLAUDE, limits: limits() });
    await openApp(page);
    await (await openMenu(page)).getByRole("button", { name: "Settings" }).click();
    const desk = machine(page, "desk");
    await desk.getByRole("button", { name: "Change the limits on desk" }).click();
    await expect(desk.getByRole("form", { name: "Limits on desk" })).toBeVisible();
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
  });
});
