// End to end: the app in a browser, a real hub, two real bridges (laptop and
// desk) and real bd repos. The tests share one world and run in order; each
// builds on the state the previous one left. The personal list is the hub's
// own; the bead tests use opted-in repos.

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
    { name: "chores", prefix: "lp", optedIn: true, human: ["Water the plants", "Renew the library card", "Buy stamps", "Fix the gate latch"] },
    { name: "garden", prefix: "ga", optedIn: true, human: ["Order compost for the beds", "Prune the roses"], other: ["Refactor the watering script"] },
    { name: "clients/website", prefix: "we", optedIn: false, human: ["Approve the footer text"] },
  ]);
  await world.addMachine("desk", [
    { name: "errands", prefix: "dp", optedIn: true, human: ["Call Sam about the weekend"] },
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
  // The hub's own personal list comes first, then each synced repo.
  const lists = page.getByRole("navigation", { name: "Lists" }).getByRole("button");
  await expect(lists.first()).toContainText("personal");
  await expect(page.getByRole("navigation", { name: "Lists" })).toContainText("errands");
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
  const chores = dir(w, "laptop", "chores");
  const garden = dir(w, "laptop", "garden");

  await row(page, "Buy stamps").getByRole("button", { name: "Mark done: Buy stamps" }).click();
  await expect.poll(async () => (await w.show(chores, id(w, "Buy stamps"))).status).toBe("closed");
  expect((await w.show(chores, id(w, "Buy stamps"))).close_reason).toBe("Done in Meat Mode");

  await row(page, "Fix the gate latch").locator(".row-body").click();
  const detail = page.locator("#detail");
  const until = new Date(Date.now() + 3 * 24 * 3600 * 1000);
  until.setUTCHours(8, 30, 0, 0);
  await detail.getByLabel("Pick a time").fill(until.toISOString().slice(0, 16));
  await detail.getByRole("button", { name: "Snooze", exact: true }).click();
  await expect
    .poll(async () => {
      const d = (await w.show(chores, id(w, "Fix the gate latch"))).defer_until;
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
  const chores = dir(w, "laptop", "chores");
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
    .poll(async () => (await w.show(chores, bead)).comments?.map((c) => c.text))
    .toContain("Card is in the drawer.");

  expect((await w.show(chores, bead)).status).toBe("open");
  const kinds = (await w.events(chores, bead)).map((e) => e.event_type);
  expect(kinds).not.toContain("closed");
});

test("undo after replay: the bead closes, then reopens", async ({ page, w }) => {
  const chores = dir(w, "laptop", "chores");
  const bead = id(w, "Water the plants");
  await open(page, w);

  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect.poll(() => actionStatus(w, "done", bead), { timeout: 5_000 }).toBe("applied");
  expect((await w.show(chores, bead)).status).toBe("closed");
  // The toast is still up (6 seconds); its Undo now posts a reopen.
  await page.getByRole("button", { name: "Undo" }).click();
  await expect.poll(async () => (await w.show(chores, bead)).status).toBe("open");
  expect(await actionStatus(w, "reopen", bead)).toBe("applied");
  const kinds = (await w.events(chores, bead)).map((e) => e.event_type);
  expect(kinds).toContain("closed");
  expect(kinds).toContain("reopened");
  await expect(row(page, "Water the plants")).not.toContainText("Waiting for sync");
});

test("a stopped bridge catches up on queued actions when it restarts", async ({ page, w }) => {
  const garden = dir(w, "laptop", "garden");
  const chores = dir(w, "laptop", "chores");
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
  expect((await w.show(chores, id(w, "Renew the library card"))).status).toBe("closed");
});

test("a reminder reaches ntfy with the title and a link to the item", async ({ w }) => {
  const chores = dir(w, "laptop", "chores");
  const due = new Date(Date.now() + 6_000).toISOString().replace(/\.\d+Z$/, "Z");
  const bead = await w.create(chores, "Feed the cat", "-l", "human", "--due", due);
  const key = `laptop/projects/chores/${bead}`;

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
  // The personal list is the hub's, not a repo on a machine.
  await expect(repos).not.toContainText("Always synced");
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

// ---------- the personal list, kept on the hub ----------

test("the personal list works with every bridge stopped and survives a hub restart", async ({ page, w }) => {
  await w.stopBridge("laptop");
  await w.stopBridge("desk");
  await open(page, w);

  const input = page.getByRole("textbox", { name: "New task" });
  await input.click();
  await input.pressSequentially("Book the dentist tomorrow");
  // The personal list is the default.
  await expect(page.locator("#qa-list")).toHaveValue("hub/personal");
  await input.press("Enter");
  await expect(input).toHaveValue("");
  await input.pressSequentially("Return the library books");
  await input.press("Enter");
  await expect(row(page, "Book the dentist")).toBeVisible();
  await expect(row(page, "Book the dentist")).not.toContainText("Waiting for sync");

  // Done, snooze and a note, each applied by the hub with no bridge.
  await row(page, "Return the library books").getByRole("button", { name: "Mark done: Return the library books" }).click();
  await row(page, "Book the dentist").locator(".row-body").click();
  const detail = page.locator("#detail");
  await detail.getByLabel("Add a note").fill("Ask about a morning slot.");
  await detail.getByRole("button", { name: "Save note" }).click();
  const until = new Date(Date.now() + 2 * 24 * 3600 * 1000);
  until.setUTCHours(8, 30, 0, 0);
  await detail.getByLabel("Pick a time").fill(until.toISOString().slice(0, 16));
  await detail.getByRole("button", { name: "Snooze", exact: true }).click();

  type Item = { key: string; bridge: string; title: string; status: string; pending?: boolean; deferUntil?: string; comments: { author: string; text: string }[] };
  const personal = async () =>
    (await w.api<{ items: Item[] }>("/api/state")).items.filter((i) => i.bridge === "hub");
  const check = async () => {
    const items = await personal();
    const dentist = items.find((i) => i.title === "Book the dentist");
    const books = items.find((i) => i.title === "Return the library books");
    expect(dentist).toMatchObject({ status: "open" });
    expect(dentist?.pending).toBeFalsy();
    expect(Date.parse(dentist!.deferUntil!)).toBe(until.getTime());
    expect(dentist!.comments.map((c) => [c.author, c.text])).toEqual([[`${USER}-app`, "Ask about a morning slot."]]);
    expect(books).toMatchObject({ status: "closed" });
  };
  await expect.poll(async () => (await personal()).find((i) => i.title === "Book the dentist")?.deferUntil).toBeTruthy();
  await check();
  const applied = (await actions(w)).filter((a) => a.bridge === "hub");
  expect(applied.length).toBe(5);
  expect(applied.every((a) => a.status === "applied")).toBe(true);

  await w.restartHub();
  await check();
  await page.reload();
  await everything(page);
  await expect(row(page, "Book the dentist")).toBeVisible();

  w.startBridge("laptop");
  w.startBridge("desk");
});

test("an agent adds a personal to-do with a machine token", async ({ page, w }) => {
  const { token } = await w.api<{ token: string }>("/api/bridges", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name: "agents" }),
  });
  const todo = { title: "Sign the permission slip", note: "The school needs it by Friday.", author: "planner" };
  const post = () =>
    fetch(`${w.hubURL}/bridge/v1/personal`, {
      method: "POST",
      headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
      body: JSON.stringify({ id: "5b0c6f1e-2a3d-4c5b-8e6f-7a8b9c0d1e2f", ...todo }),
    });
  const first = await post();
  expect(first.status).toBe(201);
  const { key } = (await first.json()) as { key: string };
  expect(key).toMatch(/^hub\/personal\//);
  // Posting the same id again adds nothing.
  expect((await post()).status).toBe(200);
  const bad = await fetch(`${w.hubURL}/bridge/v1/personal`, {
    method: "POST",
    headers: { authorization: "Bearer mm_not-a-token", "content-type": "application/json" },
    body: JSON.stringify(todo),
  });
  expect(bad.status).toBe(401);

  // A due personal to-do reminds like any other item.
  const due = new Date(Date.now() + 6_000).toISOString().replace(/\.\d+Z$/, "Z");
  const timed = await fetch(`${w.hubURL}/bridge/v1/personal`, {
    method: "POST",
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
    body: JSON.stringify({ title: "Take the bins out", due }),
  });
  expect(timed.status).toBe(201);
  const { key: binsKey } = (await timed.json()) as { key: string };
  await expect
    .poll(() => w.ntfy.requests.filter((r) => r.headers["title"] === "Take the bins out").length, { timeout: 30_000 })
    .toBe(1);
  const req = w.ntfy.requests.find((r) => r.headers["title"] === "Take the bins out")!;
  expect(new URL(String(req.headers["click"])).searchParams.get("item")).toBe(binsKey);
  expect(req.at).toBeGreaterThanOrEqual(Date.parse(due));

  await open(page, w);
  await expect(row(page, todo.title)).toHaveCount(1);
  await row(page, todo.title).locator(".row-body").click();
  await expect(page.locator("#detail")).toContainText(todo.note);
  await expect(page.locator("#detail")).toContainText("planner");
});

test("an older bridge's personal repo moves to the hub once", async ({ page, w }) => {
  // An older bridge still sends its personal repo; this one speaks its API.
  const { token } = await w.api<{ token: string }>("/api/bridges", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name: "old-laptop" }),
  });
  const call = (method: string, path: string, body: unknown) =>
    fetch(w.hubURL + path, {
      method,
      headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
      body: JSON.stringify(body),
    }).then((r) => expect(r.status, `${method} ${path}`).toBe(204));
  const now = new Date().toISOString();
  const closed = new Date(Date.now() - 3600_000).toISOString();
  const set = [
    { id: "op-1", title: "Mend the fence", status: "open", priority: 2, createdAt: now, updatedAt: now,
      comments: [{ id: "c1", author: USER, text: "Use the spare boards.", createdAt: now }] },
    { id: "op-2", title: "Post the birthday card", status: "closed", priority: 2, createdAt: now, updatedAt: now, closedAt: closed, closeReason: "Done in Meat Mode", comments: [] },
  ];
  await call("PUT", "/bridge/v1/repos", [{ name: "personal", personal: true, optedIn: true }]);
  await call("PUT", "/bridge/v1/items?repo=personal", set);
  // A rerun, with a change made in beads meanwhile, adds and changes nothing.
  await call("PUT", "/bridge/v1/items?repo=personal", [{ ...set[0], title: "Mend the fence today" }, set[1]]);

  type Item = { key: string; bridge: string; title: string; status: string; comments: { text: string }[] };
  const items = (await w.api<{ items: Item[]; repos: { bridge: string; name: string }[] }>("/api/state"));
  const moved = items.items.filter((i) => ["Mend the fence", "Mend the fence today", "Post the birthday card"].includes(i.title));
  expect(moved.map((i) => [i.key, i.status])).toEqual([
    ["hub/personal/op-1", "open"],
    ["hub/personal/op-2", "closed"],
  ]);
  expect(moved[0].comments.map((c) => c.text)).toEqual(["Use the spare boards."]);
  expect(items.repos.filter((r) => r.name === "personal").map((r) => r.bridge)).toEqual(["hub"]);

  await open(page, w);
  await expect(row(page, "Mend the fence")).toHaveCount(1);
  await expect(row(page, "Mend the fence")).toContainText("personal");
});
