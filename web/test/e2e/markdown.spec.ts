import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Page } from "@playwright/test";

test.use({ viewport: { width: 1280, height: 800 } });

const DESCRIPTION = [
  "## Steps",
  "",
  "Fill the **watering can** and check the [schedule](https://example.com/plants).",
  "Then the herbs.",
  "",
  "- [ ] Basil",
  "- [x] Mint",
  "",
  "<script>alert(1)</script> <img src=x onerror=alert(1)>",
  "",
  "![The fern](https://example.com/fern.png) [bad](javascript:alert(1))",
  "",
  "| Pot | Water |",
  "|:----|------:|",
  "| Fern | 200 ml |",
].join("\n");

/** Collects the page's CSP violations from the first script on. */
async function watchCSP(page: Page) {
  await page.addInitScript(() => {
    const w = window as unknown as { cspViolations: string[] };
    w.cspViolations = [];
    document.addEventListener("securitypolicyviolation", (e) => w.cspViolations.push(`${e.violatedDirective} ${e.blockedURI}`));
  });
}

test("a description and notes render as Markdown, with no CSP violation", async ({ page, hub }) => {
  await watchCSP(page);
  const it = hub.find("hub/personal/pe-1")!;
  it.description = DESCRIPTION;
  it.comments = [{ id: "c1", author: hub.user, text: "Done with *most* of it.\nOne pot left.", createdAt: hub.now() }];
  await openApp(page);
  await row(page, "Water the plants").locator(".row-body").click();
  const md = page.locator("#detail .why .md");

  await expect(md.locator(".md-h")).toHaveText("Steps");
  await expect(md.locator("strong")).toHaveText("watering can");
  const link = md.getByRole("link", { name: "schedule" });
  await expect(link).toHaveAttribute("href", "https://example.com/plants");
  await expect(link).toHaveAttribute("target", "_blank");
  await expect(link).toHaveAttribute("rel", "noopener noreferrer");
  // A single newline is a line break; a task item is its text.
  await expect(md.locator("p").nth(1).locator("br")).toHaveCount(1);
  await expect(md.locator("li")).toHaveText(["[ ] Basil", "[x] Mint"]);
  // Raw HTML shows as text, an image is a link, and a javascript: link is its text alone.
  await expect(md).toContainText("<script>alert(1)</script> <img src=x onerror=alert(1)>");
  await expect(md.locator("script, img, [onerror]")).toHaveCount(0);
  await expect(md.getByRole("link", { name: "The fern" })).toHaveAttribute("href", "https://example.com/fern.png");
  await expect(md.getByRole("link", { name: "bad" })).toHaveCount(0);
  await expect(md).toContainText("bad");
  await expect(md.locator("td.md-right")).toHaveText("200 ml");

  const note = page.locator("#detail .note .md");
  await expect(note.locator("em")).toHaveText("most");
  await expect(note.locator("br")).toHaveCount(1);

  await axe(page);
  expect(await page.evaluate(() => (window as unknown as { cspViolations: string[] }).cspViolations)).toEqual([]);
});

test("an agent's description renders as Markdown in its run card", async ({ page, hub }) => {
  await watchCSP(page);
  hub.find("home/garden/ga-1")!.description = "Pick a supplier:\n\n1. **Local** farm\n2. `online` shop";
  await openApp(page);
  await row(page, "Order compost for the beds").locator(".row-body").click();
  const md = page.locator("#detail .md").first();
  await expect(md.locator("ol li")).toHaveText(["Local farm", "online shop"]);
  await expect(md.locator("code")).toHaveText("online");
  expect(await page.evaluate(() => (window as unknown as { cspViolations: string[] }).cspViolations)).toEqual([]);
});

test("a plain-text description keeps its lines and blank lines", async ({ page, hub }) => {
  hub.find("hub/personal/pe-1")!.description = "Front pots\nBack pots\n\nNot the cactus.";
  await openApp(page);
  await row(page, "Water the plants").locator(".row-body").click();
  const md = page.locator("#detail .why .md");
  await expect(md.locator("p")).toHaveCount(2);
  const [first, second] = await md.locator("p").evaluateAll((ps) => ps.map((p) => p.getBoundingClientRect()));
  const lineHeight = await md.locator("p").first().evaluate((p) => parseFloat(getComputedStyle(p).lineHeight));
  // The gap between paragraphs is one line, as the blank line was before.
  expect(Math.round(second.top - first.bottom)).toBe(Math.round(lineHeight));
  expect(Math.round(first.height)).toBe(Math.round(2 * lineHeight));
});
