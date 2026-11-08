// End to end: one beads database cloned on two machines through a Dolt
// remote, both bridges syncing it. The hub shows it as one list, each task
// once, from desk (paired first) while it is online and from laptop when it
// is not, and an action on the list reaches the machine it shows.

import { test as base, expect, type Page } from "@playwright/test";
import { hasBd, World } from "./world.ts";

const BD_MISSING = "bd is not on PATH: install beads 1.3.x to run the end-to-end suite (CI sets E2E_REQUIRE_BD=1 and installs it)";
const PLANTS = "Water the plants";
const FERN = "Repot the fern";

interface StateRepo {
  bridge: string;
  name: string;
  group?: string;
  standby?: boolean;
}
interface State {
  repos: StateRepo[];
  items: { key: string; title: string; status: string }[];
}

base.describe.configure({ mode: "serial" });
if (!process.env.E2E_REQUIRE_BD) base.skip(!hasBd(), BD_MISSING);

let world: World | undefined;
let failed = false;

const test = base.extend<{ w: World }>({
  w: async ({}, use) => {
    if (!world) throw new Error("world not set up");
    await use(world);
  },
});

base.beforeAll(async () => {
  if (process.env.E2E_REQUIRE_BD && !hasBd()) throw new Error(BD_MISSING);
  // A bridge counts as offline 3 s after it was last heard from.
  const w = (world = await World.create({ hubEnv: { MEAT_MODE_ONLINE_WINDOW: "3s" } }));
  await w.addMachine("desk", [{ name: "garden", prefix: "gd", optedIn: true, human: [PLANTS, FERN] }]);
  await w.addMachine("laptop", []);
  await w.cloneRepo("desk", "garden", "laptop");
  for (const name of ["desk", "laptop"]) {
    const { token } = await w.api<{ token: string }>("/api/bridges", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name }),
    });
    await w.pair(name, w.hubURL, token);
    w.startBridge(name);
  }
});

base.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) failed = true;
});

base.afterAll(async () => {
  if (failed && world) console.log(world.logs());
  await world?.dispose();
  world = undefined;
});

function row(page: Page, title: string) {
  return page.locator("#list .row", { has: page.locator(".row-title", { hasText: title }) });
}

async function everything(page: Page, w: World) {
  await page.goto(w.hubURL + "/");
  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Everything/ }).click();
  await expect(page.locator("#view-title")).toHaveText("Everything");
}

/** Which machine's copy of garden the hub shows, once both are linked. */
async function shownFrom(w: World): Promise<string> {
  const st = await w.api<State>("/api/state");
  const garden = st.repos.filter((r) => r.name === "projects/garden");
  if (garden.length !== 2 || garden.some((r) => r.group !== "desk/projects/garden")) return "unlinked";
  return garden.filter((r) => !r.standby).map((r) => r.bridge).join(",");
}

test("two clones of one beads database show as one list, from the machine paired first", async ({ page, w }) => {
  await expect.poll(() => shownFrom(w), { timeout: 30_000 }).toBe("desk");
  await everything(page, w);
  for (const t of [PLANTS, FERN]) await expect(row(page, t)).toHaveCount(1);
  const lists = page.getByRole("navigation", { name: "Lists" });
  await expect(lists.getByRole("button", { name: /garden/ })).toHaveCount(1);
  const st = await w.api<State>("/api/state");
  expect(st.items.filter((i) => i.title === PLANTS).map((i) => i.key)).toEqual([`desk/projects/garden/${w.ids.get(PLANTS)}`]);

  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  // Machines show in the order they were paired.
  await expect(page.locator(".machine .m-linked")).toHaveText(["Same list on laptop · The list shows this copy", "Same list on desk · The list shows desk's copy"]);
});

test("with the first machine off, the list shows the other's copy, and a done reaches it", async ({ page, w }) => {
  await everything(page, w);
  await w.stopBridge("desk");
  await expect.poll(() => shownFrom(w), { timeout: 30_000 }).toBe("laptop");
  // The open page moves to laptop's copy with the list, without a reload.
  const key = (k: string) => `${k}/projects/garden/${w.ids.get(PLANTS)}`;
  await expect(row(page, PLANTS)).toHaveAttribute("data-key", key("laptop"));
  const st = await w.api<State>("/api/state");
  expect(st.items.filter((i) => i.title === FERN).map((i) => i.key)).toEqual([`laptop/projects/garden/${w.ids.get(FERN)}`]);

  await row(page, PLANTS).getByRole("button", { name: `Mark done: ${PLANTS}` }).click();
  const laptop = w.machine("laptop").repos.garden;
  await expect.poll(async () => (await w.show(laptop, w.ids.get(PLANTS)!)).status, { timeout: 30_000 }).toBe("closed");

  // The clones sync through their Dolt remote, and desk takes the list back.
  const desk = w.machine("desk").repos.garden;
  await w.sync(laptop, desk);
  expect((await w.show(desk, w.ids.get(PLANTS)!)).status).toBe("closed");
  w.startBridge("desk");
  await expect.poll(() => shownFrom(w), { timeout: 30_000 }).toBe("desk");
  // desk's first report after its restart brings the close it pulled.
  await expect
    .poll(async () => (await w.api<State>("/api/state")).items.filter((i) => i.title === PLANTS).map((i) => [i.key, i.status]), { timeout: 30_000 })
    .toEqual([[key("desk"), "closed"]]);
  await expect(row(page, FERN)).toHaveCount(1);
  await expect(row(page, PLANTS)).toHaveAttribute("data-key", key("desk"));
});
