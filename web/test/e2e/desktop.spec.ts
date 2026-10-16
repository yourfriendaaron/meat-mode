import { test, expect, openApp, row, axe } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

const groupLabels = (page: import("@playwright/test").Page) => page.locator("#list .group-head h2");

test("navigates views and lists", async ({ page, hub }) => {
  await openApp(page);
  await expect(page.locator("#view-title")).toHaveText("Today");
  await expect(groupLabels(page)).toHaveText([/Past due/, /^Today/, /This evening/, /Done today/]);
  await expect(page.locator(".dayload")).toContainText("4 open today");
  await expect(page.getByRole("button", { name: /From agents/ })).toContainText("2 new");

  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Upcoming/ }).click();
  await expect(page.locator("#view-title")).toHaveText("Upcoming");
  await expect(groupLabels(page)).toHaveText([/Tomorrow/, /This week/, /Later/]);
  await expect(row(page, "Buy seeds").locator(".chip-orange")).toHaveText("Tomorrow 9:00");
  await expect(row(page, "Plan the garden layout").locator(".chip-purple")).toHaveText("Fri, Oct 23");

  await page.getByRole("button", { name: /From agents/ }).click();
  await expect(page.locator("#list .row")).toHaveCount(3);
  await expect(row(page, "Order compost for the beds").locator(".chip-pink")).toHaveText("New");
  await expect(row(page, "Approve the new footer text").locator(".chip-cyan")).toHaveText("New reply");
  await expect(row(page, "Order compost for the beds").locator(".check-agent")).toHaveCount(1);

  await page.getByRole("button", { name: /Everything/ }).click();
  await expect(groupLabels(page).filter({ hasText: "No date" })).toHaveCount(1);

  await page.getByRole("navigation", { name: "Lists" }).getByRole("button", { name: /garden/ }).click();
  await expect(page.locator("#view-title")).toHaveText("garden");
  await expect(page.locator("#list .row-title")).toHaveText(["Order compost for the beds", "Check the tomato labels"]);
  void hub;
});

test("opens detail, marks it seen, and saves a note", async ({ page, hub }) => {
  await openApp(page);
  await page.getByRole("button", { name: /From agents/ }).click();
  await row(page, "Approve the new footer text").locator(".row-body").click();
  const detail = page.locator("#detail");
  await expect(detail.getByRole("heading", { name: "Approve the new footer text" })).toBeVisible();
  await expect(detail.locator(".why-label")).toHaveText("planner asks because");
  await expect(detail.locator(".note-unread")).toHaveCount(1);
  await expect(detail.getByRole("button", { name: "Won't do" })).toBeVisible();
  await expect.poll(() => hub.requests).toContain("POST /api/items/home%2Fwebsite%2Fwe-1/seen");
  await expect(row(page, "Approve the new footer text").locator(".chip-cyan")).toHaveCount(0);
  expect(new URL(page.url()).searchParams.get("item")).toBe("home/website/we-1");

  await detail.getByLabel("Add a note").fill("Approved, ship it.");
  await detail.getByRole("button", { name: "Save note" }).click();
  await expect(detail.locator(".note-own")).toHaveCount(2);
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "note", key: "home/website/we-1", payload: { text: "Approved, ship it." } });
  await expect(row(page, "Approve the new footer text")).toContainText("Waiting for sync");
});

test("?item= opens that item", async ({ page, hub }) => {
  void hub;
  await page.goto("/?item=" + encodeURIComponent("home/garden/ga-1"));
  await expect(page.locator("#detail").getByRole("heading", { name: "Order compost for the beds" })).toBeVisible();
});

test("keyboard: j/k, Enter, x, s, n, Escape", async ({ page, hub }) => {
  await openApp(page);
  await page.keyboard.press("j");
  await expect(row(page, "Renew the library card")).toHaveClass(/is-selected/);
  await page.keyboard.press("j");
  await expect(row(page, "Water the plants")).toHaveClass(/is-selected/);
  await page.keyboard.press("j");
  await page.keyboard.press("k");
  await expect(row(page, "Water the plants")).toHaveClass(/is-selected/);

  await page.keyboard.press("Enter");
  await expect(page.locator("#detail h2#d-title")).toHaveText("Water the plants");
  await page.keyboard.press("Escape");
  await expect(page.locator("#detail .detail-empty")).toBeVisible();

  await page.keyboard.press("x");
  await expect(page.locator("#toast")).toContainText("Done: Water the plants");
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "home/personal/pe-1" });
  await expect(page.locator(".group-done .row-title")).toContainText(["Water the plants"]);
  // Selection moved on to the next open row.
  await expect(row(page, "Call Sam about the weekend")).toHaveClass(/is-selected/);

  await page.keyboard.press("s");
  const dialog = page.getByRole("dialog", { name: "Snooze" });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await page.keyboard.press("s");
  await dialog.getByRole("button", { name: /Tomorrow/ }).click();
  await expect(dialog).toBeHidden();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "snooze",
    key: "home/personal/pe-3",
    payload: { until: "2026-10-15T09:00:00.000Z", previous: null },
  });

  await page.keyboard.press("n");
  const input = page.getByRole("textbox", { name: "New task" });
  await expect(input).toBeFocused();
  // Keys typed into the input are text, not shortcuts.
  await input.pressSequentially("Buy compost tomorrow #gar");
  await expect(page.locator("#qa-chips")).toContainText("Due Tomorrow 9:00");
  await expect(page.locator("#qa-chips")).toContainText("garden");
  await expect(page.locator("#qa-backdrop mark")).toHaveText(["tomorrow", "#gar"]);
  await expect(page.locator("#qa-list")).toHaveValue("home/garden");
  await input.press("Enter");
  await expect(input).toHaveValue("");
  const add = hub.posted.at(-1)!;
  expect(add).toMatchObject({ kind: "add", bridge: "home", repo: "garden", payload: { title: "Buy compost", due: "2026-10-15T09:00:00.000Z" } });
  expect(add.id).toMatch(/^[0-9a-f-]{36}$/);
});

test("Undo cancels a pending Done", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(page.locator(".group-done")).toContainText("Water the plants");
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page.locator(".group-pastdue")).toContainText("Water the plants");
  expect(hub.requests.some((r) => /^POST \/api\/actions\/.+\/cancel$/.test(r))).toBe(true);
  expect(hub.posted.map((a) => a.kind)).toEqual(["done"]);
});

test("Undo after the bridge applied it posts a reopen", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(page.locator(".group-done")).toContainText("Water the plants");
  hub.applyAll();
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page.locator(".group-pastdue")).toContainText("Water the plants");
  expect(hub.posted.map((a) => a.kind)).toEqual(["done", "reopen"]);
});

test("Won't do and Snooze all are undoable; Done today reopens", async ({ page, hub }) => {
  await openApp(page);
  await page.getByRole("button", { name: "Snooze all to tomorrow" }).click();
  await expect(page.locator(".group-pastdue")).toHaveCount(0);
  await expect(page.locator("#toast")).toContainText("Snoozed 2 tasks to Tomorrow 9:00");
  hub.applyAll();
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page.locator(".group-pastdue .row")).toHaveCount(2);
  const back = hub.posted.slice(-2);
  expect(back.every((a) => a.kind === "snooze" && a.payload.until === null)).toBe(true);

  await row(page, "Order compost for the beds").locator(".row-body").click();
  await page.locator("#detail").getByRole("button", { name: "Won't do" }).click();
  await expect(page.locator(".group-done")).toContainText("Order compost for the beds");
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page.locator(".group-evening")).toContainText("Order compost for the beds");

  await row(page, "Sweep the porch").getByRole("button", { name: "Reopen", exact: true }).click();
  await expect(page.locator(".group-done")).toHaveCount(0);
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "reopen", key: "home/personal/pe-6" });
});

test("sync pill and popover", async ({ page, hub }) => {
  await openApp(page);
  const pill = page.locator(".sidebar .sync-pill");
  await expect(pill).toHaveText("Synced");
  await row(page, "Water the plants").getByRole("button", { name: /Mark done/ }).click();
  await expect(pill).toHaveText("1 waiting");
  await row(page, "Call Sam about the weekend").getByRole("button", { name: /Mark done/ }).click();
  hub.applyAll();
  hub.posted.length = 0;
  await pill.click();
  const pop = page.getByRole("dialog", { name: "Recent actions" });
  await expect(pop.locator(".act")).toHaveCount(2);
  await expect(pop.locator(".act-applied")).toHaveCount(2);
  await expect(pop.locator(".act-what").first()).toHaveCSS("text-decoration-line", "line-through");
  await page.keyboard.press("Escape");
  await expect(pop).toBeHidden();

  hub.bridges[0].lastSeen = "2026-10-14T09:00:00Z";
  await page.reload();
  await expect(pill).toHaveText("Agent machine offline");
});

test("theme toggle persists, and axe passes in both themes", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await row(page, "Order compost for the beds").locator(".row-body").click();
  await page.keyboard.press("j");
  await axe(page);

  await page.locator(".sidebar").getByRole("button", { name: "Light theme" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.locator("body")).toHaveCSS("background-color", "rgb(255, 251, 235)");
  await axe(page);

  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.locator(".sidebar").getByRole("button", { name: "Dark theme" }).click();
  await expect(page.locator("body")).toHaveCSS("background-color", "rgb(40, 42, 54)");
});

test("axe passes on settings and the snooze dialog", async ({ page, hub }) => {
  await openApp(page);
  await page.keyboard.press("j");
  await page.keyboard.press("s");
  await axe(page);
  await page.keyboard.press("Escape");

  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await expect(page.locator("#view-title")).toHaveText("Settings");
  await expect(page.getByRole("heading", { name: "Reminders" })).toBeVisible();
  await expect(page.locator("#qa-form")).toBeHidden();
  await page.getByLabel("Machine name").fill("laptop");
  await page.getByRole("button", { name: "Add machine" }).click();
  await expect(page.locator("#pair-cmd")).toHaveText(/^meat-mode bridge pair http:\/\/127\.0\.0\.1:\d+ test-token$/);
  await expect(page.locator(".machine")).toHaveCount(2);
  await axe(page);
  await page.locator(".machine", { hasText: "laptop" }).getByRole("button", { name: "Revoke" }).click();
  await page.locator(".machine", { hasText: "laptop" }).getByRole("button", { name: "Revoke" }).click();
  await expect(page.locator(".machine")).toHaveCount(1);
  expect(hub.requests).toContain("DELETE /api/bridges/laptop");
});

test("snooze to a picked time; failed actions show their error", async ({ page, hub }) => {
  await openApp(page);
  await page.getByRole("button", { name: /Everything/ }).click();
  await expect(row(page, "Fix the gate latch").locator(".row-error")).toHaveText("bd: issue is locked");
  await row(page, "Fix the gate latch").locator(".row-body").click();
  const detail = page.locator("#detail");
  // A time in the past is refused, marked invalid, and posts nothing.
  await detail.getByLabel("Pick a time").fill("2026-10-13T08:30");
  await detail.getByRole("button", { name: "Snooze", exact: true }).click();
  await expect(detail.getByLabel("Pick a time")).toHaveAttribute("aria-invalid", "true");
  expect(hub.posted).toHaveLength(0);
  await detail.getByLabel("Pick a time").fill("2026-10-20T08:30");
  await detail.getByRole("button", { name: "Snooze", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "snooze",
    key: "home/personal/pe-7",
    payload: { until: "2026-10-20T08:30:00.000Z", previous: null },
  });
  await expect(row(page, "Fix the gate latch").locator(".chip-orange")).toHaveText("Tue, Oct 20");
});

test("pairing token is shown once and leaves the screen with Settings", async ({ page, hub }) => {
  hub.eventRetryMs = 200; // live refreshes while Settings is open
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await page.getByLabel("Machine name").fill("laptop");
  await page.getByRole("button", { name: "Add machine" }).click();
  const box = page.locator(".token-box");
  await expect(box).toBeVisible();
  // A live refresh while Settings stays open keeps the token on screen.
  const states = () => hub.requests.filter((r) => r === "GET /api/state").length;
  const before = states();
  await expect.poll(states).toBeGreaterThan(before);
  await expect(box).toBeVisible();
  // Leaving Settings and coming back does not.
  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Today/ }).click();
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await expect(page.locator("#view-title")).toHaveText("Settings");
  await expect(page.locator(".token-box")).toHaveCount(0);
  // A second token appears, and Done closes it.
  await page.getByLabel("Machine name").fill("desk");
  await page.getByRole("button", { name: "Add machine" }).click();
  await expect(page.locator(".token-box")).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(page.locator(".token-box")).toHaveCount(0);
});

test("sync popover says when actions could not be loaded", async ({ page, hub }) => {
  await openApp(page);
  hub.failActions = true;
  await page.locator(".sidebar .sync-pill").click();
  const pop = page.getByRole("dialog", { name: "Recent actions" });
  await expect(pop).toContainText("Could not load actions.");
  await expect(pop.locator(".act")).toHaveCount(0);
  await page.keyboard.press("Escape");
  hub.failActions = false;
  await page.locator(".sidebar .sync-pill").click();
  await expect(pop).toContainText("No actions yet.");
});

test("live updates: a change event re-fetches state", async ({ page, hub }) => {
  hub.eventRetryMs = 200;
  await openApp(page);
  hub.items.push({ ...hub.items[0], key: "home/personal/pe-9", id: "pe-9", title: "Feed the cat" });
  await expect(row(page, "Feed the cat")).toBeVisible();
});
