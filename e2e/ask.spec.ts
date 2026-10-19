// End to end through an agent run: a real hub, one machine whose bridge runs
// the fake agent (internal/bridge/fakeagent) in place of Claude Code, and real
// bd. The fake does what words in the bead's newest note say, so each test
// steers it from the note box as a user would. The tests share one world and
// run in order: the first switches the repo on in Settings and its agents on at the machine.

import { test as base, expect, type Page } from "@playwright/test";
import { hasBd, World, type Captured } from "./world.ts";

interface Run {
  id: string;
  state: string;
  agent: string;
  reason?: string;
  queued?: number;
  count: number;
}

const BD_MISSING = "bd is not on PATH: install beads 1.3.x to run the end-to-end suite (CI sets E2E_REQUIRE_BD=1 and installs it)";
const AGENT = "planner";
const JOURNEY = "asks from the sheet, follows up mid-run, gets the reply, answers and the agent closes the task";
const PLANTS = "Water the plants";
const METER = "Read the usage meter";

base.describe.configure({ mode: "serial" });
if (!process.env.E2E_REQUIRE_BD) base.skip(!hasBd(), BD_MISSING);

let world: World | undefined;
let failed = false;
/** Set once the first test has switched the repo and its agents on. */
let ready = false;

const test = base.extend<{ w: World }>({
  w: async ({}, use, info) => {
    if (!world) throw new Error("world not set up");
    if (!ready && info.title !== JOURNEY) throw new Error("these tests build on each other: run the whole of ask.spec.ts");
    await use(world);
  },
});

base.beforeAll(async () => {
  if (process.env.E2E_REQUIRE_BD && !hasBd()) throw new Error(BD_MISSING);
  const w = (world = await World.create());
  // The agent's beads are planner's, so the app names the agent planner.
  const m = await w.addMachine("laptop", [{ name: "chores", prefix: "ch", optedIn: false, human: [], commit: true }]);
  for (const t of [PLANTS, METER]) await w.create(m.repos.chores, t, "-l", "human", "--actor", AGENT);
  const { token } = await w.api<{ token: string }>("/api/bridges", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name: "laptop" }),
  });
  await w.pair("laptop", w.hubURL, token);
  w.startBridge("laptop", await w.fakeAgentEnv("2s"));
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

function key(w: World, title: string): string {
  const id = w.ids.get(title);
  if (!id) throw new Error(`no bead id for ${title}`);
  return `laptop/projects/chores/${id}`;
}

function row(page: Page, title: string) {
  return page.locator("#list .row", { has: page.locator(".row-title", { hasText: title }) });
}

/** Opens an item's sheet from the Everything list. */
async function openItem(page: Page, w: World, title: string) {
  await page.goto(w.hubURL + "/");
  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Everything/ }).click();
  await row(page, title).locator(".row-body").click();
  await expect(page.locator("#d-title")).toHaveText(title);
}

/** The item's run as the hub holds it. */
async function runOf(w: World, k: string): Promise<Run | undefined> {
  const state = await w.api<{ items: { key: string; run?: Run }[] }>("/api/state");
  return state.items.find((i) => i.key === k)?.run;
}

/** The ntfy notifications for one item with that title. */
function pushes(w: World, k: string, title: string): Captured[] {
  return w.ntfy.requests.filter((r) => r.headers["title"] === title && new URL(String(r.headers["click"])).searchParams.get("item") === k);
}

/** Checks a run-end notification's link and its Reply action. */
function expectLinks(w: World, req: Captured, k: string) {
  const link = `${w.hubURL}/?item=${encodeURIComponent(k)}`;
  expect(req.headers["click"]).toBe(link);
  expect(req.headers["actions"]).toContain(`view, Reply, ${link}&reply=1, clear=true`);
}

// ---------- tests ----------

test(JOURNEY, async ({ page, w }) => {
  const k = key(w, PLANTS);
  const chores = w.machine("laptop").repos.chores;

  // 1. Sync the repo in the app and let agents work in it on the machine.
  await page.goto(w.hubURL + "/");
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  const sync = page.getByRole("switch", { name: /^Sync chores / });
  const agents = page.getByRole("switch", { name: /^Agents can work in chores / });
  // The app cannot switch agents on: it shows the command for the machine instead.
  await expect(agents).toHaveCount(0);
  await sync.click();
  await expect(sync).toHaveAttribute("aria-checked", "true");
  await expect(page.getByText(/^meat-mode bridge agents on \S*chores$/)).toBeVisible();
  await w.setAgents("laptop", chores, true);
  await expect(agents).toHaveAttribute("aria-checked", "true");
  await expect(page.getByText("Waiting for laptop")).toHaveCount(0);
  ready = true;

  // 2. Ask from the sheet with a note; the fake goes slow, then replies.
  await openItem(page, w, PLANTS);
  const card = page.locator("#detail .run-card");
  const note = page.locator("#note-input");
  await note.fill("Take it slow and reply");
  await page.getByRole("button", { name: "Save and ask agent" }).click();
  await expect(card).toHaveAttribute("data-run", "working");
  await expect(card.locator(".run-line")).toContainText(AGENT);
  await expect(row(page, PLANTS)).toHaveAttribute("data-run", "working");
  await expect(row(page, PLANTS).locator(".run-line")).toContainText(AGENT);

  // 3. A follow-up right away is on its way (the hub counts it queued on the
  // live run), then reaches the same run.
  // Every render of the notes from here on is recorded: the notes never
  // drop out, and only the follow-up carries the marker.
  await page.evaluate(() => {
    const seen: string[][] = [];
    (window as unknown as { seenNotes: string[][] }).seenNotes = seen;
    const take = () =>
      seen.push([...document.querySelectorAll("#detail .note")].map((n) => `${n.querySelector("p")?.textContent ?? ""}${n.hasAttribute("data-run") ? " [on its way]" : ""}`));
    take();
    new MutationObserver(take).observe(document.querySelector("#detail")!, { childList: true, subtree: true, attributes: true });
  });
  await note.fill("Use the blue watering can");
  await page.getByRole("button", { name: `Send to ${AGENT}` }).click();
  const followUp = page.locator("#detail .note", { hasText: "Use the blue watering can" });
  await expect(followUp.getByText(`On its way to ${AGENT}`)).toBeVisible();
  // Reached: the note is shown without the marker while the run still works.
  await expect(page.locator("#detail .note:not([data-run])", { hasText: "Use the blue watering can" })).toBeVisible();
  await expect(card).toHaveAttribute("data-run", "working");
  const seen = await page.evaluate(() => (window as unknown as { seenNotes: string[][] }).seenNotes);
  const views = seen.map((v) => v.join(" | ")).filter((v, i, all) => v !== all[i - 1]);
  expect(views).toEqual([
    "Take it slow and reply",
    "Take it slow and reply | Use the blue watering can [on its way]",
    "Take it slow and reply | Use the blue watering can",
  ]);
  const first = (await runOf(w, k))!;
  expect(first).toMatchObject({ state: "working", agent: AGENT, count: 1 });
  expect(first.id).not.toMatch(/^pending-/);
  expect(first.queued ?? 0).toBe(0);

  // 4. The run ends waiting on the user, with one push.
  await expect(card).toHaveAttribute("data-run", "waiting");
  await expect(card.locator(".run-line")).toContainText(`${AGENT} replied`);
  await expect(page.locator("#detail .note-agent", { hasText: "Which plants?" })).toBeVisible();
  await expect(page.locator("#detail .note-agent .note-author").last()).toHaveText(AGENT);
  expect(await runOf(w, k)).toMatchObject({ id: first.id, state: "waiting", count: 1 });
  await expect.poll(() => pushes(w, k, `${AGENT} replied`).length).toBe(1);
  const replied = pushes(w, k, `${AGENT} replied`)[0];
  expect(replied.body).toBe(PLANTS);
  expectLinks(w, replied, k);

  // 5. Reply asking it to close: a new run closes the bead.
  await note.fill("They are by the window, please close it");
  await page.getByRole("button", { name: `Reply to ${AGENT}` }).click();
  await expect(card).toHaveAttribute("data-run", "done");
  await expect(card.locator(".run-line")).toContainText(`${AGENT} finished`);
  expect(await runOf(w, k)).toMatchObject({ state: "done", count: 2 });
  expect((await w.show(chores, w.ids.get(PLANTS)!)).status).toBe("closed");
  await expect(row(page, PLANTS)).toHaveClass(/is-done/);
  await expect.poll(() => pushes(w, k, `${AGENT} finished`).length).toBe(1);
  expectLinks(w, pushes(w, k, `${AGENT} finished`)[0], k);
  // Still one reply push: the second run's end did not repeat the first's.
  expect(pushes(w, k, `${AGENT} replied`)).toHaveLength(1);
});

test("a run that hits the usage limit stops, and Retry starts a new one", async ({ page, w }) => {
  const k = key(w, METER);
  const stoppedTitle = `${AGENT} stopped: usage limit reached`;

  // The bead's title has no note on top of it, so the fake reads "usage" there.
  await openItem(page, w, METER);
  const card = page.locator("#detail .run-card");
  await card.getByRole("button", { name: "Ask agent" }).click();
  await expect(card).toHaveAttribute("data-run", "stopped");
  await expect(card.locator(".run-line")).toContainText(`${AGENT} stopped · It hit its usage limit.`);
  await expect(card.locator(".run-line")).toContainText("It resets at");
  const stopped = (await runOf(w, k))!;
  expect(stopped).toMatchObject({ state: "stopped", reason: "usage-limit", count: 1 });
  await expect.poll(() => pushes(w, k, stoppedTitle).length).toBe(1);
  expectLinks(w, pushes(w, k, stoppedTitle)[0], k);

  // A plain note steers the next run, then Retry wakes the agent again.
  await page.locator("#note-input").fill("Then reply");
  await page.getByRole("button", { name: "Save note" }).click();
  await expect(page.locator("#detail .note-own", { hasText: "Then reply" })).toBeVisible();
  await card.getByRole("button", { name: "Retry" }).click();
  await expect(card).toHaveAttribute("data-run", "waiting");
  await expect(card.locator(".run-line")).toContainText(`${AGENT} replied`);
  const retried = (await runOf(w, k))!;
  expect(retried).toMatchObject({ state: "waiting", count: 2 });
  expect(retried.id).not.toBe(stopped.id);
  await expect.poll(() => pushes(w, k, `${AGENT} replied`).length).toBe(1);
  expect(pushes(w, k, stoppedTitle)).toHaveLength(1);
});
