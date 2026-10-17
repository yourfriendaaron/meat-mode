// End to end: the app in a browser, a real hub, two real bridges (laptop and
// desk) and real bd repos. The tests share one world and run in order; each
// builds on the state the previous one left.

import { test as base, expect, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { hasBd, NTFY_TOPIC, USER, World, type Issue } from "./world.ts";

interface Action {
  id: string;
  kind: string;
  bridge: string;
  repo: string;
  beadId?: string;
  status: string;
  payload: { due?: string | null };
}

const BD_MISSING = "bd is not on PATH: install beads 1.3.x to run the end-to-end suite (CI sets E2E_REQUIRE_BD=1 and installs it)";

base.describe.configure({ mode: "serial" });
if (!process.env.E2E_REQUIRE_BD) base.skip(!hasBd(), BD_MISSING);

let world: World | undefined;
let failed = false;
/** desk's repos as bd lists them right after pairing. */
let deskBefore: Record<string, Issue[]> = {};

const test = base.extend<{ w: World }>({
  w: async ({}, use) => {
    if (!world) throw new Error("world not set up");
    await use(world);
  },
});

base.beforeAll(async () => {
  if (process.env.E2E_REQUIRE_BD && !hasBd()) throw new Error(BD_MISSING);
  world = await World.create();
  await world.addMachine("laptop", [
    { name: "personal", prefix: "lp", optedIn: false, human: ["Water the plants", "Renew the library card", "Buy stamps", "Fix the gate latch"] },
    { name: "garden", prefix: "ga", optedIn: true, human: ["Order compost for the beds", "Prune the roses"], other: ["Refactor the watering script"] },
    { name: "clients/website", prefix: "we", optedIn: false, human: ["Approve the footer text"] },
  ]);
  await world.addMachine("desk", [
    { name: "personal", prefix: "dp", optedIn: false, human: ["Call Sam about the weekend"] },
    { name: "kitchen", prefix: "ki", optedIn: true, human: ["Descale the kettle"] },
    { name: "garage", prefix: "gr", optedIn: false, human: ["Sort the toolbox"] },
  ]);
});

base.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) failed = true;
});

base.afterAll(async () => {
  if (failed && world) console.log(world.logs());
  await world?.dispose();
  world = undefined;
});

// ---------- helpers ----------

function row(page: Page, title: string) {
  return page.locator("#list .row", { has: page.locator(".row-title", { hasText: title }) });
}

async function everything(page: Page) {
  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Everything/ }).click();
  await expect(page.locator("#view-title")).toHaveText("Everything");
}

async function open(page: Page, w: World) {
  await page.goto(w.hubURL + "/");
  await everything(page);
}

function dir(w: World, machine: string, repo: string): string {
  return w.machine(machine).repos[repo];
}

function id(w: World, title: string): string {
  const v = w.ids.get(title);
  if (!v) throw new Error(`no bead id for ${title}`);
  return v;
}

async function actions(w: World): Promise<Action[]> {
  return w.api<Action[]>("/api/actions?limit=200");
}

/** Waits for the action on that bead to reach a status on the hub. */
async function actionStatus(w: World, kind: string, beadId: string): Promise<string | undefined> {
  return (await actions(w)).find((a) => a.kind === kind && a.beadId === beadId)?.status;
}

async function deskSnapshot(w: World): Promise<Record<string, Issue[]>> {
  const out: Record<string, Issue[]> = {};
  for (const [name, d] of Object.entries(w.machine("desk").repos)) out[name] = await w.list(d);
  return out;
}

async function bridgeSeenAfter(w: World, name: string, t: number) {
  await expect
    .poll(async () => {
      const bridges = await w.api<{ name: string; lastSeen?: string }[]>("/api/bridges");
      const b = bridges.find((x) => x.name === name);
      return b?.lastSeen ? Date.parse(b.lastSeen) >= t : false;
    })
    .toBe(true);
}

// ---------- tests ----------

test("pairs two bridges through Settings and shows both machines' items", async ({ page, w }) => {
  await page.goto(w.hubURL + "/");
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  for (const name of ["laptop", "desk"]) {
    await page.getByLabel("Machine name").fill(name);
    await page.getByRole("button", { name: "Add machine" }).click();
    const cmd = page.locator("#pair-cmd");
    await expect(cmd).toContainText(`meat-mode bridge pair ${w.hubURL} `);
    await expect(page.locator(".token-box")).toContainText(name);
    const [, , , url, token] = (await cmd.textContent())!.trim().split(/\s+/);
    expect(await w.pair(name, url, token)).toContain(`as bridge "${name}"`);
    w.startBridge(name);
  }
  await expect(page.locator(".machine")).toHaveCount(2);

  await everything(page);
  for (const t of ["Water the plants", "Renew the library card", "Order compost for the beds", "Prune the roses", "Call Sam about the weekend", "Descale the kettle"]) {
    await expect(row(page, t)).toBeVisible();
  }
  // Not labelled human, or in a repo that is not opted in: never shown.
  for (const t of ["Refactor the watering script", "Approve the footer text", "Sort the toolbox"]) {
    await expect(row(page, t)).toHaveCount(0);
  }
  await expect(page.getByRole("navigation", { name: "Lists" })).toContainText("personalon desk");
  await expect(page.locator(".sidebar .sync-pill")).toHaveText("Synced");
  deskBefore = await deskSnapshot(w);
});

test("quick add with a date and #list creates a bead in that repo", async ({ page, w }) => {
  await open(page, w);
  const input = page.getByRole("textbox", { name: "New task" });
  await input.click();
  await input.pressSequentially("Buy compost tomorrow #garden");
  await expect(page.locator("#qa-list")).toHaveValue("laptop/projects/garden");
  await input.press("Enter");
  await expect(input).toHaveValue("");

  const garden = dir(w, "laptop", "garden");
  let bead: Issue | undefined;
  await expect
    .poll(async () => {
      bead = (await w.list(garden)).find((i) => i.title === "Buy compost");
      return !!bead;
    })
    .toBe(true);
  w.ids.set("Buy compost", bead!.id);
  const full = await w.show(garden, bead!.id);
  expect(full.labels).toContain("human");
  const add = (await actions(w)).find((a) => a.kind === "add" && a.repo === "projects/garden");
  expect(add).toMatchObject({ bridge: "laptop", status: "applied", beadId: bead!.id });
  expect(Date.parse(full.due_at!)).toBe(Date.parse(add!.payload.due!));
  const tomorrow9 = new Date();
  tomorrow9.setUTCDate(tomorrow9.getUTCDate() + 1);
  tomorrow9.setUTCHours(9, 0, 0, 0);
  expect(Date.parse(full.due_at!)).toBe(tomorrow9.getTime());

  // The provisional row gives way to the real bead.
  await expect(row(page, "Buy compost")).toHaveCount(1);
  await expect(row(page, "Buy compost")).not.toContainText("Waiting for sync");
});

test("done, snooze, note and won't do reach bd", async ({ page, w }) => {
  await open(page, w);
  const personal = dir(w, "laptop", "personal");
  const garden = dir(w, "laptop", "garden");

  await row(page, "Buy stamps").getByRole("button", { name: "Mark done: Buy stamps" }).click();
  await expect.poll(async () => (await w.show(personal, id(w, "Buy stamps"))).status).toBe("closed");
  expect((await w.show(personal, id(w, "Buy stamps"))).close_reason).toBe("Done in Meat Mode");

  await row(page, "Fix the gate latch").locator(".row-body").click();
  const detail = page.locator("#detail");
  const until = new Date(Date.now() + 3 * 24 * 3600 * 1000);
  until.setUTCHours(8, 30, 0, 0);
  await detail.getByLabel("Pick a time").fill(until.toISOString().slice(0, 16));
  await detail.getByRole("button", { name: "Snooze", exact: true }).click();
  await expect
    .poll(async () => {
      const d = (await w.show(personal, id(w, "Fix the gate latch"))).defer_until;
      return d ? Date.parse(d) : null;
    })
    .toBe(until.getTime());

  await row(page, "Order compost for the beds").locator(".row-body").click();
  await detail.getByLabel("Add a note").fill("Two bags is enough.");
  await detail.getByRole("button", { name: "Save note" }).click();
  await expect
    .poll(async () => (await w.show(garden, id(w, "Order compost for the beds"))).comments?.map((c) => [c.author, c.text]))
    .toContainEqual([`${USER}-app`, "Two bags is enough."]);

  await row(page, "Prune the roses").locator(".row-body").click();
  await detail.getByRole("button", { name: "Won't do" }).click();
  await expect.poll(async () => (await w.show(garden, id(w, "Prune the roses"))).status).toBe("closed");

  await expect.poll(async () => (await w.api<{ unapplied: number }>("/api/state")).unapplied).toBe(0);
  await expect(page.locator(".sidebar .sync-pill")).toHaveText("Synced");
});

test("laptop's actions never reach desk; desk's own actions do", async ({ page, w }) => {
  const laptopActions = (await actions(w)).filter((a) => a.bridge === "laptop");
  expect(laptopActions.length).toBeGreaterThanOrEqual(5);
  expect(laptopActions.every((a) => a.status === "applied")).toBe(true);
  // desk never pulled any of them: its bridge logs every action it sees.
  const deskLog = w.machine("desk").proc!.output;
  for (const a of laptopActions) expect(deskLog).not.toContain(a.id);
  expect(deskLog).not.toContain("ignoring an action for another bridge");
  // And desk's repos are exactly as they were after pairing.
  expect(await deskSnapshot(w)).toEqual(deskBefore);

  await open(page, w);
  await row(page, "Descale the kettle").getByRole("button", { name: "Mark done: Descale the kettle" }).click();
  const kitchen = dir(w, "desk", "kitchen");
  await expect.poll(async () => (await w.show(kitchen, id(w, "Descale the kettle"))).status).toBe("closed");
  const deskDone = (await actions(w)).find((a) => a.bridge === "desk" && a.kind === "done")!;
  expect(w.machine("laptop").proc!.output).not.toContain(deskDone.id);
});

test("undo before replay: the bead never closes", async ({ page, w }) => {
  const personal = dir(w, "laptop", "personal");
  const bead = id(w, "Renew the library card");
  await w.stopBridge("laptop");
  await open(page, w);

  await row(page, "Renew the library card").getByRole("button", { name: "Mark done: Renew the library card" }).click();
  await expect(page.locator(".sidebar .sync-pill")).toHaveText("1 waiting");
  await page.getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => actionStatus(w, "done", bead)).toBe("cancelled");
  await expect(page.locator(".sidebar .sync-pill")).toHaveText("Synced");

  const restarted = Date.now();
  w.startBridge("laptop");
  await bridgeSeenAfter(w, "laptop", restarted);
  // Prove the restarted bridge has replayed since: a later note on the same
  // bead must reach bd first.
  await row(page, "Renew the library card").locator(".row-body").click();
  await page.locator("#detail").getByLabel("Add a note").fill("Card is in the drawer.");
  await page.locator("#detail").getByRole("button", { name: "Save note" }).click();
  await expect
    .poll(async () => (await w.show(personal, bead)).comments?.map((c) => c.text))
    .toContain("Card is in the drawer.");

  expect((await w.show(personal, bead)).status).toBe("open");
  const kinds = (await w.events(personal, bead)).map((e) => e.event_type);
  expect(kinds).not.toContain("closed");
});

test("undo after replay: the bead closes, then reopens", async ({ page, w }) => {
  const personal = dir(w, "laptop", "personal");
  const bead = id(w, "Water the plants");
  await open(page, w);

  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect.poll(() => actionStatus(w, "done", bead), { timeout: 5_000 }).toBe("applied");
  expect((await w.show(personal, bead)).status).toBe("closed");
  // The toast is still up (6 seconds); its Undo now posts a reopen.
  await page.getByRole("button", { name: "Undo" }).click();
  await expect.poll(async () => (await w.show(personal, bead)).status).toBe("open");
  expect(await actionStatus(w, "reopen", bead)).toBe("applied");
  const kinds = (await w.events(personal, bead)).map((e) => e.event_type);
  expect(kinds).toContain("closed");
  expect(kinds).toContain("reopened");
  await expect(row(page, "Water the plants")).not.toContainText("Waiting for sync");
});

test("a stopped bridge catches up on queued actions when it restarts", async ({ page, w }) => {
  const garden = dir(w, "laptop", "garden");
  const personal = dir(w, "laptop", "personal");
  await w.stopBridge("laptop");
  await open(page, w);
  const pill = page.locator(".sidebar .sync-pill");

  await row(page, "Order compost for the beds").getByRole("button", { name: "Mark done: Order compost for the beds" }).click();
  await row(page, "Renew the library card").getByRole("button", { name: "Mark done: Renew the library card" }).click();
  await expect(pill).toHaveText("2 waiting");
  await expect(row(page, "Order compost for the beds")).toContainText("Waiting for sync");
  // Queued: nothing has reached bd.
  expect((await w.show(garden, id(w, "Order compost for the beds"))).status).toBe("open");

  w.startBridge("laptop");
  await expect(pill).toHaveText("Synced");
  expect((await w.show(garden, id(w, "Order compost for the beds"))).status).toBe("closed");
  expect((await w.show(personal, id(w, "Renew the library card"))).status).toBe("closed");
});

test("a reminder reaches ntfy with the title and a link to the item", async ({ w }) => {
  const personal = dir(w, "laptop", "personal");
  const due = new Date(Date.now() + 6_000).toISOString().replace(/\.\d+Z$/, "Z");
  const bead = await w.create(personal, "Feed the cat", "--due", due);
  const key = `laptop/personal/${bead}`;

  await expect
    .poll(() => w.ntfy.requests.filter((r) => r.headers["title"] === "Feed the cat").length, { timeout: 30_000 })
    .toBe(1);
  const req = w.ntfy.requests.find((r) => r.headers["title"] === "Feed the cat")!;
  expect(req.method).toBe("POST");
  expect(req.url).toBe(`/${NTFY_TOPIC}`);
  expect(req.body).toBe("Due now");
  const click = new URL(String(req.headers["click"]));
  expect(click.origin).toBe(w.hubURL);
  expect(click.searchParams.get("item")).toBe(key);
  // It fired at the due time, not before.
  expect(req.at).toBeGreaterThanOrEqual(Date.parse(due));
});

test("a repo switched on in Settings syncs, and switched off leaves the app", async ({ page, w }) => {
  await page.goto(w.hubURL + "/");
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  const repos = page.getByRole("list", { name: "Repos on laptop" });
  // Every repo the bridge found is listed, nested ones by their path.
  await expect(repos).toContainText("projects/clients/website");
  await expect(repos).toContainText("Always synced");
  const sw = repos.getByRole("switch", { name: "Sync website (projects/clients/website)" });
  await expect(sw).toHaveAttribute("aria-checked", "false");

  await sw.click();
  await expect(sw).toHaveAttribute("aria-checked", "true");
  await expect(repos).not.toContainText("Waiting for laptop");
  await everything(page);
  await expect(row(page, "Approve the footer text")).toBeVisible();
  // The bridge keeps the choice in its own config.
  const cfg = JSON.parse(await readFile(join(w.machine("laptop").home, ".config", "meat-mode", "bridge.json"), "utf8"));
  expect(cfg.sync).toEqual({ "projects/clients/website": true });

  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await sw.click();
  await expect(sw).toHaveAttribute("aria-checked", "false");
  await expect(repos).not.toContainText("Waiting for laptop");
  await everything(page);
  await expect(row(page, "Approve the footer text")).toHaveCount(0);
});
