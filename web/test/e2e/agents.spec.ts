import { test, expect, axe, wizard, openWizardAt, openApp } from "./setup.ts";
import type { Page } from "@playwright/test";

async function lightTheme(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem("meat-mode.theme", "light");
    document.documentElement.dataset.theme = "light";
  });
}

async function noHorizontalScroll(page: Page) {
  const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll).toBeLessThanOrEqual(inner);
}

/** What Finish says of one step. */
function outcome(page: Page, title: string) {
  return page.locator(".wiz-summary li", { has: page.locator(".wiz-sum-title", { hasText: new RegExp(`^${title}$`) }) }).locator(".wiz-outcome");
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 }, permissions: ["clipboard-read", "clipboard-write"] });

  test("pairs a machine, waits for its hello, then chooses repos and copies the instructions", async ({ page, hub, baseURL }) => {
    await openWizardAt(page, hub, "agents");
    const w = wizard(page);
    await expect(w.heading).toHaveText("Agent machines");
    await expect(w.region).toContainText("Do you use AI agents with a task tracker?");
    await axe(page);

    await w.region.getByRole("button", { name: "Yes, add a machine" }).click();
    await expect(page.getByLabel("Machine name")).toBeFocused();
    await page.getByLabel("Machine name").fill("Laptop");
    await w.region.getByRole("button", { name: "Add machine" }).click();
    await expect(w.region.getByRole("alert")).toHaveText("Use 1 to 32 lowercase letters, digits or dashes.");
    await page.getByLabel("Machine name").fill("laptop");
    await w.region.getByRole("button", { name: "Add machine" }).click();

    await expect(page.locator("#agents-pair-cmd")).toHaveText(`meat-mode bridge pair ${baseURL} test-token`);
    await expect(page.locator("#agents-install")).toHaveText(
      "mkdir -p ~/.local/bin && curl -fsSL -o ~/.local/bin/meat-mode https://github.com/yourfriendaaron/meat-mode/releases/latest/download/meat-mode-linux-amd64 && chmod +x ~/.local/bin/meat-mode",
    );
    await page.getByLabel("Platform").selectOption("darwin-arm64");
    await expect(page.getByLabel("Platform")).toBeFocused();
    await expect(page.locator("#agents-install")).toContainText("/meat-mode-darwin-arm64 ");
    await page.getByLabel("Platform").selectOption("go");
    await expect(page.locator("#agents-install")).toHaveText("go install github.com/yourfriendaaron/meat-mode/cmd/meat-mode@latest");
    const status = w.region.getByRole("status");
    await expect(status).toHaveText("Waiting for laptop to connect…");
    await axe(page);
    await lightTheme(page);
    await axe(page);

    hub.hello("laptop", ["projects/garden", "work/notes"]);
    await expect(status).toHaveText("Connected. laptop is talking to this hub.");

    await w.next.click();
    await expect(w.heading).toHaveText("Choose repos");
    await expect(w.region).toContainText("Only tasks labelled human appear");
    const garden = w.region.getByRole("switch", { name: "Sync garden (projects/garden)" });
    await expect(garden).toHaveAttribute("aria-checked", "false");
    await garden.click();
    await expect(garden).toHaveAttribute("aria-checked", "true");
    expect(hub.posted.find((a) => a.kind === "sync")).toMatchObject({ bridge: "laptop", repo: "projects/garden", payload: { on: true } });
    await axe(page);

    // Agents switch on at the machine: the step shows the command, with no switch for it.
    hub.applyAll();
    hub.repos.find((r) => r.name === "projects/garden")!.pending = false;
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    const gardenRow = w.region.locator(".m-repo", { has: page.getByRole("switch", { name: "Sync garden (projects/garden)" }) });
    await expect(gardenRow).toContainText("Agents are off here");
    await expect(gardenRow.locator("code")).toHaveText("meat-mode bridge agents on projects/garden");
    await expect(gardenRow.getByRole("switch", { name: /Agents can work/ })).toHaveCount(0);
    await gardenRow.getByRole("button", { name: "Copy the command for projects/garden" }).click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("meat-mode bridge agents on projects/garden");
    await axe(page);

    await w.next.click();
    await expect(w.heading).toHaveText("Tell your agents");
    const text = page.locator("#wiz-agents-text");
    await expect(text).toContainText('This machine is paired with it as "laptop".');
    await expect(text).toContainText('bd create "Approve the footer text" -l human');
    await expect(text).toContainText('meat-mode add "Renew the library card"');
    await expect(text).toContainText("## Tasks for alex");
    await w.region.getByRole("button", { name: "Copy instructions" }).click();
    const copied = await page.evaluate(() => navigator.clipboard.readText());
    expect(copied).toBe(await text.textContent());
    expect(copied).not.toContain("test-token");
    expect(copied).not.toContain(baseURL!);
    await axe(page);

    await w.next.click();
    await expect(w.heading).toHaveText("All set");
    const summary = page.locator(".wiz-summary");
    await expect(outcome(page, "Agent machines")).toHaveText("Set up");
    await expect(outcome(page, "Choose repos")).toHaveText("Set up");
    await summary.getByRole("button", { name: "Tell your agents in Settings" }).click();
    await expect(page.locator("#s-agents")).toHaveText("Instructions for agents");
    await expect(page.locator("#set-agents-text")).toContainText('as "laptop"');
  });

  test("adding a machine asks for the password again in place, and says when it is wrong", async ({ page, hub, baseURL }) => {
    hub.passwordFresh = false;
    await openWizardAt(page, hub, "agents");
    const w = wizard(page);
    await w.region.getByRole("button", { name: "Yes, add a machine" }).click();
    await page.getByLabel("Machine name").fill("laptop");
    await w.region.getByRole("button", { name: "Add machine" }).click();
    const pw = w.region.getByLabel("Password");
    await expect(pw).toBeFocused();
    await expect(w.region).toContainText("Enter your password to add a machine.");
    await expect(pw).toHaveAttribute("autocomplete", "current-password");
    expect(hub.bridges.map((b) => b.name)).not.toContain("laptop");
    await axe(page);

    await pw.fill("not it");
    await w.region.getByRole("button", { name: "Add machine" }).click();
    await expect(w.region.getByRole("alert")).toHaveText("Wrong password.");
    await expect(pw).toHaveValue("");
    await expect(pw).toBeFocused();

    hub.loginStatus = 429;
    await pw.fill("still not it");
    await pw.press("Enter");
    await expect(w.region.getByRole("alert")).toHaveText("Too many tries. Wait a minute and try again.");

    hub.loginStatus = 0;
    await pw.fill(hub.password);
    await pw.press("Enter");
    await expect(page.locator("#agents-pair-cmd")).toHaveText(`meat-mode bridge pair ${baseURL} test-token`);
    expect(hub.bridgePosts.at(-1)).toEqual({ name: "laptop", password: hub.password });
    expect(hub.bridgePosts[0]).toEqual({ name: "laptop" });
  });

  test("Settings asks for the password again before it adds a machine", async ({ page, hub }) => {
    hub.passwordFresh = false;
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const section = page.locator("section", { has: page.locator("#s-mach") });
    await section.getByLabel("Machine name").fill("desk");
    await section.getByRole("button", { name: "Add machine" }).click();
    const pw = section.getByLabel("Password");
    await expect(pw).toBeFocused();
    await expect(section.getByLabel("Machine name")).toHaveValue("desk");
    await axe(page);
    await pw.fill(hub.password);
    await section.getByRole("button", { name: "Add machine" }).click();
    await expect(page.locator("#pair-cmd")).toContainText("test-token");
    await expect(section.getByLabel("Password")).toHaveCount(0);
    await expect(section.getByLabel("Machine name")).toHaveValue("");
  });

  test("No skips the agent steps", async ({ page, hub }) => {
    await openWizardAt(page, hub, "agents");
    const w = wizard(page);
    await w.region.getByRole("button", { name: "No", exact: true }).click();
    await expect(w.heading).toHaveText("All set");
    await expect(outcome(page, "Agent machines")).toHaveText("Skipped");
    await expect(page.locator(".wiz-steps")).not.toContainText("Choose repos");
  });

  test("Continue asks for a machine before moving on", async ({ page, hub }) => {
    await openWizardAt(page, hub, "agents");
    const w = wizard(page);
    await w.next.click();
    await expect(page.getByLabel("Machine name")).toBeFocused();
    await w.next.click();
    await expect(w.region.getByRole("alert")).toHaveText("Add a machine, or skip this step.");
    await expect(w.heading).toHaveText("Agent machines");
  });

  test("a machine that never connected waits on the repos step", async ({ page, hub }) => {
    await openWizardAt(page, hub, "agents");
    hub.bridges.push({ name: "desk", createdAt: hub.now() });
    await page.reload();
    const w = wizard(page);
    await expect(w.region).toContainText("Not connected yet");
    await expect(w.region).toContainText("A pairing token is shown only once.");
    await w.next.click();
    await expect(w.heading).toHaveText("Choose repos");
    await expect(w.region).toContainText("Waiting for desk to connect.");
  });

  test("Settings keeps the instructions for each machine", async ({ page, hub }) => {
    hub.bridges.push({ name: "desk", createdAt: hub.now(), lastSeen: hub.now() });
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const text = page.locator("#set-agents-text");
    await expect(text).toContainText('as "desk"');
    await page.getByLabel("Machine", { exact: true }).selectOption("home");
    await expect(text).toContainText('as "home"');
    await expect(page.getByLabel("Machine", { exact: true })).toBeFocused();
    await axe(page);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the pairing commands and instructions fit the screen", async ({ page, hub }) => {
    await openWizardAt(page, hub, "agents");
    const w = wizard(page);
    await w.region.getByRole("button", { name: "Yes, add a machine" }).click();
    await page.getByLabel("Machine name").fill("laptop");
    await w.region.getByRole("button", { name: "Add machine" }).click();
    await expect(page.locator("#agents-pair-cmd")).toBeVisible();
    await noHorizontalScroll(page);
    await axe(page);

    hub.hello("laptop", ["projects/garden"]);
    await expect(w.region.getByRole("status")).toContainText("Connected");
    await w.next.click();
    await w.next.click();
    await expect(w.heading).toHaveText("Tell your agents");
    await noHorizontalScroll(page);
    await axe(page);
  });
});
