import { test, expect, openApp, row, axe } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

const groupLabels = (page: import("@playwright/test").Page) => page.locator("#list .group-head h2");

test("navigates views and lists", async ({ page, hub }) => {
  await openApp(page);
  await expect(page.locator("#view-title")).toHaveText("Today");
  await expect(groupLabels(page)).toHaveText([/Past due/, /^Today/, /This evening/, /Tomorrow/, /This week/, /Later/, /Done today/]);
  await expect(page.locator(".group-tomorrow .row-title")).toHaveText(["Buy seeds"]);
  await expect(page.locator(".dayload")).toContainText("4 open today");
  await expect(page.getByRole("button", { name: /From agents/ })).toContainText("2 new");

  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Upcoming/ }).click();
  await expect(page.locator("#view-title")).toHaveText("Upcoming");
  await expect(groupLabels(page)).toHaveText([/Tomorrow/, /This week/, /Later/]);
  await expect(row(page, "Buy seeds").locator(".chip-snoozed")).toHaveText("Tomorrow 9:00");
  await expect(row(page, "Plan the garden layout").locator(".chip-date")).toHaveText("Fri, Oct 23");

  await page.getByRole("button", { name: /From agents/ }).click();
  await expect(page.locator("#list .row")).toHaveCount(3);
  await expect(row(page, "Order compost for the beds").locator(".chip-new")).toHaveText("New");
  await expect(row(page, "Approve the new footer text").locator(".chip-new")).toHaveText("New reply");
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
  await expect(row(page, "Approve the new footer text").locator(".chip-new")).toHaveCount(0);
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
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "hub/personal/pe-1" });
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
    key: "hub/personal/pe-3",
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

test("New task adds to the list being viewed", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  const list = page.locator("#qa-list");
  await expect(list).toHaveValue("hub/personal");

  await page.getByRole("navigation", { name: "Lists" }).getByRole("button", { name: /garden/ }).click();
  await expect(list).toHaveValue("home/garden");

  // A pick by hand holds while typing, and a #tag still wins.
  await list.selectOption("home/website");
  const input = page.getByRole("textbox", { name: "New task" });
  await input.fill("Buy twine");
  await expect(list).toHaveValue("home/website");
  await input.fill("Buy twine #garden");
  await expect(list).toHaveValue("home/garden");

  // Another view starts afresh.
  await input.fill("");
  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Today/ }).click();
  await expect(list).toHaveValue("hub/personal");
});

test("Undo cancels a pending Done", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Order compost for the beds").getByRole("button", { name: "Mark done: Order compost for the beds" }).click();
  await expect(page.locator(".group-done")).toContainText("Order compost for the beds");
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page.locator(".group-evening")).toContainText("Order compost for the beds");
  expect(hub.requests.some((r) => /^POST \/api\/actions\/.+\/cancel$/.test(r))).toBe(true);
  expect(hub.posted.map((a) => a.kind)).toEqual(["done"]);
});

test("Undo after the bridge applied it posts a reopen", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Order compost for the beds").getByRole("button", { name: "Mark done: Order compost for the beds" }).click();
  await expect(page.locator(".group-done")).toContainText("Order compost for the beds");
  hub.applyAll();
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page.locator(".group-evening")).toContainText("Order compost for the beds");
  expect(hub.posted.map((a) => a.kind)).toEqual(["done", "reopen"]);
});

test("the personal list applies at once, and Undo posts a reopen", async ({ page, hub }) => {
  await openApp(page);
  const pill = page.locator(".sidebar .sync-pill");
  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(page.locator(".group-done")).toContainText("Water the plants");
  await expect(row(page, "Water the plants")).not.toContainText("Waiting for sync");
  await expect(pill).toHaveText("Synced");
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
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "reopen", key: "hub/personal/pe-6" });
});

test("Delete removes a personal task everywhere and is undoable", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Call Sam about the weekend").locator(".row-body").click();
  const detail = page.locator("#detail");
  await expect(detail.getByRole("button", { name: "Won't do" })).toHaveCount(0);
  await detail.getByRole("button", { name: "Delete" }).click();
  await expect(detail.getByRole("heading", { name: "Call Sam about the weekend" })).toHaveCount(0);
  await expect(row(page, "Call Sam about the weekend")).toHaveCount(0);
  await expect(page.locator("#toast")).toContainText("Deleted: Call Sam about the weekend");
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "dismiss", key: "hub/personal/pe-3" });
  await page.getByRole("button", { name: /Everything/ }).click();
  await expect(row(page, "Call Sam about the weekend")).toHaveCount(0);
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(row(page, "Call Sam about the weekend")).toHaveCount(1);
});

test("sync pill and popover", async ({ page, hub }) => {
  await openApp(page);
  const pill = page.locator(".sidebar .sync-pill");
  await expect(pill).toHaveText("Synced");
  await row(page, "Order compost for the beds").getByRole("button", { name: /Mark done/ }).click();
  await expect(pill).toHaveText("1 waiting");
  await row(page, "Call Sam about the weekend").getByRole("button", { name: /Mark done/ }).click();
  await expect(pill).toHaveText("1 waiting");
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

test("Settings Appearance offers dark, light and the device's setting", async ({ page, hub }) => {
  void hub;
  await page.emulateMedia({ colorScheme: "light" });
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  const look = page.locator("section[aria-labelledby=s-look]");
  const html = page.locator("html");
  await expect(look.getByRole("radio", { name: /^Dark/ })).toBeChecked();
  await look.getByRole("radio", { name: /^Light/ }).check();
  await expect(html).toHaveAttribute("data-theme", "light");
  await look.getByRole("radio", { name: /^Match this device/ }).check();
  expect(await page.evaluate(() => localStorage.getItem("meat-mode.theme"))).toBe("system");
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(html).toHaveAttribute("data-theme", "dark");
  await axe(page);
  await page.reload();
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await expect(look.getByRole("radio", { name: /^Match this device/ })).toBeChecked();
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
    key: "hub/personal/pe-7",
    payload: { until: "2026-10-20T08:30:00.000Z", previous: null },
  });
  await expect(row(page, "Fix the gate latch").locator(".chip-snoozed")).toHaveText("Tue, Oct 20");
});

test("snooze to a typed time", async ({ page, hub }) => {
  await openApp(page);
  await page.keyboard.press("j");
  await page.keyboard.press("s");
  const dialog = page.getByRole("dialog", { name: "Snooze" });
  const typed = dialog.getByRole("textbox", { name: "Type a time" });
  const line = dialog.locator(".snooze-type-now");
  await typed.focus();
  // What it cannot read, and a time already past, are refused with a reason and post nothing.
  await typed.fill("whenever");
  await expect(line).toHaveText("Could not read that time. Try something like tomorrow 9am.");
  await typed.press("Enter");
  await expect(typed).toHaveAttribute("aria-invalid", "true");
  await typed.fill("today 8am");
  await expect(line).toHaveText("That time has passed. Pick a later one.");
  await typed.press("Enter");
  expect(hub.posted).toHaveLength(0);
  await expect(dialog).toBeVisible();
  // The time it reads shows as it is typed, and Enter snoozes to it.
  await typed.fill("8p");
  await expect(line).toHaveText("Until 20:00");
  await expect(typed).not.toHaveAttribute("aria-invalid");
  await typed.press("Enter");
  await expect(dialog).toBeHidden();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "snooze", payload: { until: "2026-10-14T20:00:00.000Z" } });
  // A reopened dialog starts empty.
  await page.keyboard.press("s");
  await expect(typed).toHaveValue("");
  await expect(line).toBeHidden();
  await page.keyboard.press("Escape");

  // The detail pane takes a typed time too, from its button.
  await page.getByRole("button", { name: /Everything/ }).click();
  await row(page, "Fix the gate latch").locator(".row-body").click();
  const detail = page.locator("#detail");
  await detail.getByRole("textbox", { name: "Type a time" }).fill("tomorrow morning");
  await expect(detail.locator(".snooze-type-now")).toHaveText("Until Tomorrow 9:00");
  await detail.getByRole("button", { name: "Snooze to the typed time" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "snooze",
    key: "hub/personal/pe-7",
    payload: { until: "2026-10-15T09:00:00.000Z", previous: null },
  });
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
  hub.items.push({ ...hub.items[0], key: "hub/personal/pe-9", id: "pe-9", title: "Feed the cat" });
  await expect(row(page, "Feed the cat")).toBeVisible();
});

test("Settings lists each machine's repos with a sync switch", async ({ page, hub }) => {
  hub.repos.push({ bridge: "home", name: "notes/journal", personal: false, optedIn: false });
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  const repos = page.getByRole("list", { name: "Repos on home" });
  // The personal list lives on the hub, not under a machine.
  await expect(repos.getByRole("listitem")).toHaveCount(3);
  await expect(repos).not.toContainText("personal");
  const sw = repos.getByRole("switch", { name: "Sync journal (notes/journal)" });
  await expect(sw).toHaveAttribute("aria-checked", "false");
  await sw.click();
  await expect(sw).toHaveAttribute("aria-checked", "true");
  await expect(repos).toContainText("Waiting for home");
  await expect(sw).toBeFocused();
  expect(hub.posted.at(-1)).toMatchObject({ kind: "sync", bridge: "home", repo: "notes/journal", payload: { on: true } });
  await axe(page);
});

test("Settings has no detail pane; a task view brings it back", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await expect(page.locator("#detail")).toBeVisible();
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await expect(page.locator("#detail")).toBeHidden();
  await page.getByRole("button", { name: /Everything/ }).click();
  await expect(page.locator("#detail")).toBeVisible();
});
