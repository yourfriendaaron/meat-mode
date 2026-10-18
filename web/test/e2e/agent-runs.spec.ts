// Ask agent: the run's card in the task sheet, the note box's pink button,
// Stop and Retry, the run on list rows and the Agents tab, the undo bar's
// Tell, New task's agent switch, Settings and the push's Reply link.

import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Page } from "@playwright/test";

const FOOTER = "home/website/we-1";
const COMPOST = "home/garden/ga-1";

/** Makes the app load the state again, as a change event from the hub would. */
async function poke(page: Page) {
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
}

/** The From agents view, where every agent item shows. */
async function agentsView(page: Page) {
  const nav = page.viewportSize()!.width < 880 ? page.locator("#tabs") : page.locator("#nav");
  await nav.getByRole("button", { name: /agents/i }).first().click();
  await expect(page.locator("#view-title")).toHaveText("From agents");
}

function sheet(page: Page) {
  return page.locator("#detail");
}

function card(page: Page) {
  return page.locator("#detail .run-card");
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("Ask agent on the card starts a run that shows at once, with Stop", async ({ page, hub }) => {
    hub.allowAgents("website");
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    await expect(card(page)).toContainText("planner asks because");
    await expect(card(page)).toContainText("The agent reads your latest notes, then acts or replies.");
    await expect(sheet(page).getByRole("button", { name: "Save and ask agent" })).toBeVisible();
    await expect(sheet(page).getByRole("button", { name: "Save note", exact: true })).toBeVisible();
    await axe(page);

    await card(page).getByRole("button", { name: "Ask agent" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "ask", key: FOOTER, payload: {} });
    await expect(card(page)).toHaveAttribute("data-run", "working");
    await expect(card(page).locator(".run-line")).toHaveText("planner · Starting");
    await expect(card(page).getByRole("button", { name: "Stop planner" })).toBeVisible();
    await expect(sheet(page).getByRole("button", { name: "Send to planner" })).toBeVisible();
    await expect(sheet(page).getByRole("button", { name: "Done", exact: true })).toBeVisible();
    await expect(sheet(page).getByRole("button", { name: "Won't do" })).toBeVisible();
    await axe(page);

    // The row carries the run too, and the Agents entry its light.
    await agentsView(page);
    await expect(row(page, "Approve the new footer text")).toHaveAttribute("data-run", "working");
    await expect(row(page, "Approve the new footer text").locator(".run-line")).toHaveText("planner · Starting");
    await expect(page.locator("#nav .tab-ic[data-run=working]")).toHaveCount(1);
    await expect(page.locator("#nav")).toContainText("an agent is working");
  });

  test("Save and ask agent sends the note with the ask; the quiet save sends only the note", async ({ page, hub }) => {
    hub.allowAgents("website");
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    const note = sheet(page).getByLabel("Add a note");
    await note.fill("Keep the copyright year.");
    await sheet(page).getByRole("button", { name: "Save note", exact: true }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "note", key: FOOTER, payload: { text: "Keep the copyright year." } });
    await expect(card(page)).not.toHaveAttribute("data-run");

    await note.fill("Use the shorter one and ship it.");
    await sheet(page).getByRole("button", { name: "Save and ask agent" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "ask", key: FOOTER, payload: { text: "Use the shorter one and ship it." } });
    await expect(note).toHaveValue("");
    await expect(card(page)).toHaveAttribute("data-run", "working");
  });

  test("a follow-up mid-run shows the running light until the agent reads it", async ({ page, hub }) => {
    hub.allowAgents("website");
    hub.setRun(FOOTER, { state: "working", activity: "Shortening the second line" });
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    await expect(card(page).locator(".run-line")).toHaveText("planner · Shortening the second line");

    // With nothing typed, Send only points at the note box.
    await sheet(page).getByRole("button", { name: "Send to planner" }).click();
    await expect(sheet(page).getByLabel("Add a note")).toBeFocused();
    expect(hub.posted).toEqual([]);

    await sheet(page).getByLabel("Add a note").fill("Oh, and keep the copyright year.");
    await sheet(page).getByRole("button", { name: "Send to planner" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "ask", key: FOOTER, payload: { text: "Oh, and keep the copyright year." } });
    const queued = sheet(page).locator(".note[data-run=working]");
    await expect(queued).toHaveCount(1);
    await expect(queued).toContainText("Oh, and keep the copyright year.");
    await expect(queued).toContainText("On its way to planner");
    await axe(page);

    // The agent reads it at its next step.
    hub.setRun(FOOTER, { queued: 0, activity: "Read your new note" });
    await poke(page);
    await expect(card(page).locator(".run-line")).toHaveText("planner · Read your new note");
    await expect(sheet(page).locator(".note[data-run]")).toHaveCount(0);
  });

  test("Stop ends the run; Retry asks again", async ({ page, hub }) => {
    hub.allowAgents("website");
    hub.setRun(FOOTER, { state: "working", activity: "Shortening the second line" });
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    await card(page).getByRole("button", { name: "Stop planner" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "stop", key: FOOTER, payload: {} });
    await expect(card(page)).toHaveAttribute("data-run", "stopped");
    await expect(card(page).locator(".run-line")).toHaveText("planner stopped · You stopped it.");
    await expect(card(page).getByRole("button", { name: "Stop planner" })).toHaveCount(0);
    await expect(sheet(page).getByRole("button", { name: "Save and ask agent" })).toBeVisible();

    await card(page).getByRole("button", { name: "Retry" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "ask", key: FOOTER, payload: {} });
    await expect(card(page)).toHaveAttribute("data-run", "working");
    await expect(card(page).locator(".run-line")).toHaveText("planner · Starting");
  });

  test("a stopped run says why in plain words, with the detail on the card only", async ({ page, hub }) => {
    hub.allowAgents("website");
    hub.setRun(FOOTER, { state: "stopped", reason: "usage-limit", detail: "The limit resets at 14:00." });
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    await expect(card(page).locator(".run-line")).toHaveText("planner stopped · It hit its usage limit. The limit resets at 14:00.");
    await agentsView(page);
    await expect(row(page, "Approve the new footer text").locator(".run-line")).toHaveText("planner stopped · It hit its usage limit.");
    await expect(card(page).getByRole("button", { name: "Retry" })).toBeVisible();
  });

  test("waiting on the user, the note box replies", async ({ page, hub }) => {
    hub.allowAgents("website");
    hub.setRun(FOOTER, { state: "waiting", activity: "Asked which wording to use" });
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    await expect(card(page).locator(".run-line")).toHaveText("planner replied · Asked which wording to use");
    // No Ask again: the reply goes through the note box.
    await expect(card(page).getByRole("button")).toHaveCount(0);
    await sheet(page).getByLabel("Add a note").fill("The shorter one.");
    await sheet(page).getByRole("button", { name: "Reply to planner" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "ask", key: FOOTER, payload: { text: "The shorter one." } });
    await expect(card(page)).toHaveAttribute("data-run", "working");
  });

  test("a run that ends flashes its row and card once", async ({ page, hub }) => {
    hub.allowAgents("website");
    hub.setRun(FOOTER, { state: "working", activity: "Writing you a reply" });
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    await agentsView(page);
    await row(page, "Approve the new footer text").locator(".row-body").click();
    await expect(card(page)).toHaveAttribute("data-run", "working");
    hub.setRun(FOOTER, { state: "done", activity: "Shortened the line and closed the task" });
    await poke(page);
    await expect(card(page)).toHaveAttribute("data-run", "done");
    await expect(card(page)).toHaveAttribute("data-arrive", "");
    await expect(row(page, "Approve the new footer text")).toHaveAttribute("data-arrive", "");
    await expect(card(page).locator(".run-line")).toHaveText("planner finished · Shortened the line and closed the task");
    await expect(card(page)).not.toHaveAttribute("data-arrive", { timeout: 3000 });
    await expect(page.locator("#nav .tab-ic")).toHaveCount(0);
  });

  test("where agents are off, the card says how to turn them on and offers no pink button", async ({ page, hub }) => {
    await openApp(page, "/?item=" + encodeURIComponent(FOOTER));
    await expect(card(page)).toContainText("Agents are off for this list.");
    await expect(sheet(page).locator(".btn-agent")).toHaveCount(0);
    await expect(sheet(page).getByRole("button", { name: "Save note" })).toHaveText("Save note");
    await axe(page);
    await card(page).getByRole("button", { name: "Open Settings" }).click();
    await expect(page.locator("#view-title")).toHaveText("Settings");
    await expect(page.locator("#s-mach")).toBeFocused();
  });

  test("a personal item shows no agent controls", async ({ page, hub }) => {
    hub.allowAgents("website");
    await openApp(page, "/?item=" + encodeURIComponent("hub/personal/pe-1"));
    await expect(sheet(page).locator(".run-card")).toHaveCount(0);
    await expect(sheet(page).locator(".btn-agent")).toHaveCount(0);
    await expect(sheet(page).getByRole("button", { name: "Save note" })).toHaveText("Save note");
  });

  test("the undo bar offers Tell after Done, then Stop while the run works", async ({ page, hub }) => {
    hub.allowAgents("garden");
    await openApp(page);
    await row(page, "Order compost for the beds").getByRole("button", { name: "Mark done: Order compost for the beds" }).click();
    const bar = page.locator("#toast");
    await expect(bar).toContainText("Done: Order compost for the beds");
    await expect(bar.getByRole("button", { name: "Undo" })).toBeVisible();
    await axe(page);
    await bar.getByRole("button", { name: "Tell planner" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "ask", key: COMPOST, payload: {} });
    await expect(bar.getByRole("button", { name: "Stop planner" })).toBeVisible();
    await expect(bar.getByRole("button", { name: "Tell planner" })).toHaveCount(0);
    await bar.getByRole("button", { name: "Stop planner" }).click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "stop", key: COMPOST, payload: {} });
  });

  test("Won't do offers Tell too, but not where agents are off", async ({ page, hub }) => {
    hub.allowAgents("garden");
    await openApp(page, "/?item=" + encodeURIComponent(COMPOST));
    await sheet(page).getByRole("button", { name: "Won't do" }).click();
    await expect(page.locator("#toast").getByRole("button", { name: "Tell planner" })).toBeVisible();

    await page.goto("/?item=" + encodeURIComponent(FOOTER));
    await sheet(page).getByRole("button", { name: "Done", exact: true }).click();
    await expect(page.locator("#toast")).toContainText("Done: Approve the new footer text");
    await expect(page.locator("#toast .btn-agent")).toHaveCount(0);
  });

  test("New task offers an agent on a list that allows one, and Add turns pink", async ({ page, hub }) => {
    hub.allowAgents("garden");
    await openApp(page);
    const input = page.getByRole("textbox", { name: "New task" });
    const ask = page.getByRole("switch", { name: "An agent starts on it" });
    const add = page.locator("#qa-add");

    // Personal, the default list: off and disabled, with the reason.
    await expect(page.locator("#qa-list")).toHaveValue("hub/personal");
    await expect(ask).toBeDisabled();
    await expect(ask).toHaveAttribute("aria-checked", "false");
    await expect(page.locator("#qa-agent")).toContainText("Personal to-dos have no agent.");
    await expect(add).toHaveText("Add");

    await page.locator("#qa-list").selectOption("home/garden");
    await expect(ask).toBeEnabled();
    await expect(ask).toHaveAttribute("aria-checked", "true");
    await expect(add).toHaveText("Add and ask agent");
    await expect(add).toHaveClass(/btn-agent/);
    await axe(page);

    await input.fill("Find a cheaper soil sensor");
    await add.click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "add", bridge: "home", repo: "garden", payload: { title: "Find a cheaper soil sensor", ask: true } });
    await agentsView(page);
    await expect(row(page, "Find a cheaper soil sensor")).toHaveAttribute("data-run", "working");
    await expect(row(page, "Find a cheaper soil sensor").locator(".run-line")).toHaveText("agent · Starting");

    // Switched off, Add is plain and posts no ask.
    await page.locator("#qa-list").selectOption("home/garden");
    await ask.click();
    await expect(ask).toHaveAttribute("aria-checked", "false");
    await expect(add).toHaveText("Add");
    await input.fill("Label the seed trays");
    await add.click();
    const plain = hub.posted.at(-1)!;
    expect(plain).toMatchObject({ kind: "add", repo: "garden", payload: { title: "Label the seed trays" } });
    expect(plain.payload.ask).toBeUndefined();

    // A repo with agents off shows the switch disabled with how to turn them on.
    await page.locator("#qa-list").selectOption("home/website");
    await expect(ask).toBeDisabled();
    await expect(page.locator("#qa-agent")).toContainText("Agents are off for this list. Turn them on in Settings.");
  });

  test("New task shows no agent switch while no list allows agents", async ({ page, hub }) => {
    void hub;
    await openApp(page);
    await expect(page.locator("#qa-agent")).toBeHidden();
  });

  test("Settings: Agents can work here switches per repo, and waits for the machine", async ({ page, hub }) => {
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const sw = page.getByRole("switch", { name: "Agents can work in website (website)" });
    await expect(sw).toHaveAttribute("aria-checked", "false");
    await expect(page.locator("[data-section=runs]")).toContainText(
      "Agents start only from your pink taps, one per task, and each run has a time limit set on its machine.",
    );
    await expect(page.locator("[data-section=runs]")).toContainText("No agent is working now.");
    await axe(page);
    await sw.click();
    expect(hub.posted.at(-1)).toMatchObject({ kind: "agents", bridge: "home", repo: "website", payload: { on: true } });
    await expect(sw).toHaveAttribute("aria-checked", "true");
    await expect(page.locator(".m-repo", { has: sw }).getByRole("status")).toHaveText("Waiting for home");

    // A repo that does not sync cannot let agents in.
    hub.repos.find((r) => r.name === "garden")!.optedIn = false;
    await poke(page);
    await expect(page.getByRole("switch", { name: "Agents can work in garden (garden)" })).toBeDisabled();
  });

  test("Settings: Stop all stops every working run", async ({ page, hub }) => {
    hub.allowAgents("website");
    hub.allowAgents("garden");
    hub.setRun(FOOTER, { state: "working", activity: "Shortening the second line" });
    hub.setRun(COMPOST, { state: "working", activity: "Comparing suppliers" });
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const section = page.locator("[data-section=runs]");
    await expect(section).toContainText("2 agents working");
    await expect(section).toContainText("planner · Approve the new footer text");
    await axe(page);
    await section.getByRole("button", { name: "Stop all" }).click();
    await expect(section).toContainText("No agent is working now.");
    expect(hub.posted.filter((a) => a.kind === "stop").map((a) => a.key).sort()).toEqual([COMPOST, FOOTER].sort());
  });

  test("a push's Reply link opens the item at its note box", async ({ page, hub }) => {
    hub.allowAgents("website");
    hub.setRun(FOOTER, { state: "waiting" });
    await page.goto("/?item=" + encodeURIComponent(FOOTER) + "&reply=1");
    await expect(page.locator("#detail h2#d-title")).toHaveText("Approve the new footer text");
    await expect(sheet(page).getByLabel("Add a note")).toBeFocused();
    await expect(sheet(page).getByRole("button", { name: "Reply to planner" })).toBeVisible();
    expect(new URL(page.url()).searchParams.get("reply")).toBeNull();
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("a working row lifts into a card, and long text wraps without overflow", async ({ page, hub }) => {
    hub.allowAgents("website");
    const it = hub.find(FOOTER)!;
    it.title = "Approve the new footer text for every page of the site, including the archived pages that still link to the old address";
    hub.setRun(FOOTER, {
      state: "working",
      activity: "Comparing the second line of the footer with the wording the legal page uses, then shortening it to fit one line on phones",
    });
    await openApp(page);
    await agentsView(page);
    const r = page.locator("#list .row[data-run=working]");
    await expect(r).toHaveCount(1);
    await expect(r.locator(".run-fx")).toHaveCount(1);
    await expect(page.locator("#tabs .tab-ic[data-run=working]")).toHaveCount(1);
    await expect(page.locator("#tabs").getByRole("button", { name: /an agent is working/ })).toBeVisible();
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    const box = await r.boundingBox();
    expect(box!.x + box!.width).toBeLessThanOrEqual(390);
    const line = await r.locator(".run-line").boundingBox();
    expect(line!.x + line!.width).toBeLessThanOrEqual(390);
    await axe(page);
  });

  test("reduced motion holds the tracer still", async ({ page, hub }) => {
    await page.emulateMedia({ reducedMotion: "reduce" });
    hub.allowAgents("website");
    hub.setRun(FOOTER, { state: "working" });
    await openApp(page);
    await agentsView(page);
    const anim = await page.locator("#list .row[data-run=working] .run-fx").evaluate((el) => getComputedStyle(el).animationName);
    expect(anim).toBe("none");
  });
});
