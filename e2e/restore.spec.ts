// End to end: a fresh hub restored from an export in the setup wizard. A
// real hub with a password and no machines, so no bridges or bd. A file from
// a newer hub is refused first; the hand-written version 1 export the store
// tests use then loads, the wizard goes on with the restored settings, and a
// second restore is refused because the hub now has data.

import { test, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { World } from "./world.ts";

const FIXTURE = new URL("../internal/store/testdata/export-v1.json", import.meta.url);

let world: World | undefined;
let failed = false;

test.beforeAll(async () => {
  world = await World.create();
});

test.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) failed = true;
});

test.afterAll(async () => {
  if (failed && world) console.log(world.logs());
  await world?.dispose();
  world = undefined;
});

test("a fresh hub is restored from an export in the setup wizard", async ({ page }) => {
  const w = world!;
  const file = await readFile(FIXTURE, "utf8");
  const wiz = page.locator("#wizard");
  const heading = wiz.locator("#wiz-h");
  const choose = (name: string, text: string) =>
    page.getByLabel("Export file").setInputFiles({ name, mimeType: "application/json", buffer: Buffer.from(text) });

  await page.goto(w.appURL);
  await expect(heading).toHaveText("Welcome");
  for (let i = 0; i < 10 && (await heading.textContent()) !== "Restore from an export"; i++) {
    const h = (await heading.textContent()) ?? "";
    // Welcome has Continue only; every later step can be skipped.
    if (h === "Welcome") await wiz.locator("[data-act=wizard-next]").click();
    else await wiz.getByRole("button", { name: "Skip" }).click();
    await expect(heading).not.toHaveText(h);
  }
  await expect(heading).toHaveText("Restore from an export");

  await test.step("a file from a newer hub is refused", async () => {
    await choose("newer.json", file.replace('"version": 1,', '"version": 2,'));
    await expect(wiz.getByRole("alert")).toContainText("the file is version 2, and this hub reads version 1 and older");
  });

  await test.step("the export loads and the step lists what to do again", async () => {
    await choose("meat-mode-export-2026-10-14.json", file);
    await expect(wiz.getByRole("status")).toHaveText("Restored 4 to-dos, 6 changes in the history and 2 reminders already sent.");
    await expect(wiz).toContainText("2 changes were still waiting for a machine and will not be sent.");
    await expect(wiz.locator(".restore-todo code")).toHaveText(["laptop"]);
    await expect(wiz.locator(".restore-todo")).toContainText("Turn the calendar feed on again");
  });

  await test.step("setup goes on with the restored settings", async () => {
    await wiz.locator("[data-act=wizard-next]").click();
    await expect(heading).toHaveText("Name and time zone");
    await expect(wiz.getByLabel("Name", { exact: true })).toHaveValue("Alex");
    await expect(wiz.getByLabel("Time zone", { exact: true })).toHaveValue("Europe/Lisbon");
    const setup = await w.api<{ status: string }>("/api/setup");
    expect(setup.status).toBe("in-use");
  });

  await test.step("the restored to-dos are there, and a second restore is refused", async () => {
    const state = await w.api<{ items: { key: string; title: string; parent?: string }[] }>("/api/state");
    expect(state.items.map((i) => i.key).sort()).toEqual(["hub/personal/mm-1", "hub/personal/mm-2", "hub/projects/garden/mm-3"]);
    expect(state.items.find((i) => i.key === "hub/personal/mm-2")?.parent).toBe("mm-1");
    await expect(
      w.api("/api/restore", { method: "POST", headers: { "content-type": "application/json" }, body: file }),
    ).rejects.toThrow(/409 .*already has data/);
  });
});
