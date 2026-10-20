// End to end: a fresh hub restored from an export in the setup wizard. Real
// hubs with a password and no machines, so no bridges or bd.
//
// The first test takes the hand-written version 1 export the store tests
// use: a file from a newer hub is refused first, the old JSON file then
// loads, the wizard goes on with the restored settings, and a second restore
// is refused because the hub now has data. The second test takes a zip: one
// hub with a to-do and a file on it exports, and a fresh hub restores it and
// serves the file again.

import { test, expect, type Browser } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { DEFAULT_PASSWORD, STORAGE_STATE, World } from "./world.ts";

const FIXTURE = new URL("../internal/store/testdata/export-v1.json", import.meta.url);

/** A 1x1 PNG. */
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=", "base64");

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

/** Opens the app at a fresh hub and skips through the wizard to the restore step. */
async function openRestoreStep(browser: Browser, appURL: string) {
  const context = await browser.newContext({ storageState: STORAGE_STATE });
  const page = await context.newPage();
  const wiz = page.locator("#wizard");
  const heading = wiz.locator("#wiz-h");
  await page.goto(appURL);
  await expect(heading).toHaveText("Welcome");
  for (let i = 0; i < 10 && (await heading.textContent()) !== "Restore from an export"; i++) {
    const h = (await heading.textContent()) ?? "";
    // Welcome has Continue only; every later step can be skipped.
    if (h === "Welcome") await wiz.locator("[data-act=wizard-next]").click();
    else await wiz.getByRole("button", { name: "Skip" }).click();
    await expect(heading).not.toHaveText(h);
  }
  await expect(heading).toHaveText("Restore from an export");
  return { context, page, wiz, heading };
}

test("a fresh hub is restored from an older hub's JSON export in the setup wizard", async ({ browser }) => {
  const w = world!;
  const file = await readFile(FIXTURE, "utf8");
  const { context, page, wiz, heading } = await openRestoreStep(browser, w.appURL);
  const choose = (name: string, text: string) =>
    page.getByLabel("Export file").setInputFiles({ name, mimeType: "application/json", buffer: Buffer.from(text) });

  await test.step("a file from a newer hub is refused", async () => {
    await choose("newer.json", file.replace('"version": 1,', '"version": 4,'));
    await expect(wiz.getByRole("alert")).toContainText("the file is version 4, and this hub reads version 3 and older");
  });

  await test.step("the export loads and the step lists what to do again", async () => {
    await choose("meat-mode-export-2026-10-14.json", file);
    await expect(wiz.getByRole("status")).toHaveText("Restored 6 to-dos, 6 changes in the history and 2 reminders already sent.");
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
    // The file's checklist comes back as sub-tasks of mm-1; the done one,
    // closed at the file's time, shows only while that is under 7 days ago.
    const keys = state.items.map((i) => i.key).filter((k) => k !== "hub/personal/mm-1.1").sort();
    expect(keys).toEqual(["hub/personal/mm-1", "hub/personal/mm-1.2", "hub/personal/mm-2", "hub/projects/garden/mm-3"]);
    expect(state.items.find((i) => i.key === "hub/personal/mm-1.2")?.parent).toBe("mm-1");
    expect(state.items.find((i) => i.key === "hub/personal/mm-2")?.parent).toBe("mm-1");
    await expect(
      w.api("/api/restore", { method: "POST", headers: { "content-type": "application/octet-stream" }, body: file }),
    ).rejects.toThrow(/409 .*already has data/);
  });
  await context.close();
});

interface Attached {
  id: string;
  name: string;
  type: string;
  size: number;
  sha256: string;
}

test("a hub's zip export, with a file, restores on a fresh hub", async ({ browser }) => {
  let source: World | undefined;
  let target: World | undefined;
  try {
    // The source hub: a to-do with a photo on it, exported as the app downloads it.
    source = await World.create();
    const login = await fetch(source.hubURL + "/api/login", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ password: DEFAULT_PASSWORD }),
    });
    expect(login.status).toBe(204);
    const cookie = login.headers.getSetCookie()[0]!.split(";")[0];
    await source.api("/api/actions", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ id: randomUUID(), kind: "add", bridge: "hub", repo: "personal", payload: { title: "Water the plants", due: null } }),
    });
    const state = await source.api<{ items: { key: string; title: string }[] }>("/api/state");
    const key = state.items.find((i) => i.title === "Water the plants")!.key;
    const photo = await source.api<Attached>(`/api/items/${encodeURIComponent(key)}/attachments?name=fern.png`, {
      method: "POST",
      headers: { "content-type": "application/octet-stream" },
      body: PNG,
    });
    expect([photo.name, photo.type, photo.size]).toEqual(["fern.png", "image/png", PNG.length]);
    const exported = await fetch(source.hubURL + "/api/export", { headers: { cookie } });
    expect(exported.status).toBe(200);
    expect(exported.headers.get("content-type")).toBe("application/zip");
    expect(exported.headers.get("content-disposition")).toMatch(/^attachment; filename="meat-mode-export-\d{4}-\d{2}-\d{2}\.zip"$/);
    const zip = Buffer.from(await exported.arrayBuffer());
    expect(zip.subarray(0, 4)).toEqual(Buffer.from("PK\x03\x04", "latin1"));
    await source.dispose();
    source = undefined;

    // A fresh hub restores it in the wizard.
    target = await World.create();
    const t = target;
    const { context, page, wiz } = await openRestoreStep(browser, t.appURL);
    await page.getByLabel("Export file").setInputFiles({ name: "meat-mode-export-2026-10-14.zip", mimeType: "application/zip", buffer: zip });
    await expect(wiz.getByRole("status")).toHaveText("Restored 1 to-do with 1 file, 1 change in the history and 0 reminders already sent.");

    // The file comes back under its id, with its bytes, and the app shows it.
    const restored = await t.api<{ items: { key: string; attachments?: Attached[] }[] }>("/api/state");
    const item = restored.items.find((i) => i.key === key)!;
    expect(item.attachments).toEqual([expect.objectContaining({ id: photo.id, name: "fern.png", size: PNG.length, sha256: photo.sha256 })]);
    await wiz.getByRole("button", { name: "Close setup" }).click();
    await page.locator("#nav").getByRole("button", { name: "Everything" }).click();
    await page.locator("#list .row", { hasText: "Water the plants" }).locator(".row-body").click();
    const files = page.locator("#detail [data-attachments]");
    await expect(files.getByRole("link", { name: "fern.png" })).toHaveAttribute("href", `/api/attachments/${photo.id}`);
    await expect.poll(() => files.locator("img").evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(1);
    await context.close();
  } catch (e) {
    console.log((source ?? target)?.logs());
    throw e;
  } finally {
    await source?.dispose();
    await target?.dispose();
  }
});
