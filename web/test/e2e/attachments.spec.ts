import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Page } from "@playwright/test";

/** A 1x1 PNG. */
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=", "base64");
const KEY = "hub/personal/pe-1";

async function openPlants(page: Page) {
  await openApp(page);
  await row(page, "Water the plants").locator(".row-body").click();
  return page.locator("#detail [data-attachments]");
}

/** Records each upload the page sends: its name parameter and content type. */
function watchUploads(page: Page): { name: string | null; type: string }[] {
  const sent: { name: string | null; type: string }[] = [];
  page.on("request", (req) => {
    const url = new URL(req.url());
    if (req.method() === "POST" && /\/attachments$/.test(url.pathname)) {
      sent.push({ name: url.searchParams.get("name"), type: req.headers()["content-type"] });
    }
  });
  return sent;
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("attaches files, shows them, and removes one with Undo", async ({ page, hub }) => {
    const sent = watchUploads(page);
    const files = await openPlants(page);
    await expect(files.locator("h3")).toHaveText("Files");
    await expect(files).toContainText("No files.");
    // A desktop gets the file picker only.
    await expect(files.getByLabel("Take a photo")).toHaveCount(0);
    await axe(page);

    await files.getByLabel("Attach a file").setInputFiles([
      { name: "fern.png", mimeType: "image/png", buffer: PNG },
      { name: "Watering plan.txt", mimeType: "text/plain", buffer: Buffer.from("Front pots on Monday.\n") },
    ]);
    const list = files.locator(".file");
    await expect(list).toHaveCount(2);
    expect(sent).toEqual([
      { name: "fern.png", type: "application/octet-stream" },
      { name: "Watering plan.txt", type: "application/octet-stream" },
    ]);
    expect(hub.find(KEY)!.attachments!.map((a) => [a.name, a.type, a.size])).toEqual([
      ["fern.png", "image/png", PNG.length],
      ["Watering plan.txt", "text/plain", 22],
    ]);

    // An image shows as a thumbnail from the hub and opens in a new tab.
    const [img, txt] = hub.find(KEY)!.attachments!;
    const image = list.nth(0).getByRole("link", { name: "fern.png" });
    await expect(image).toHaveAttribute("href", `/api/attachments/${img.id}`);
    await expect(image).toHaveAttribute("target", "_blank");
    await expect.poll(() => list.nth(0).locator("img").evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(1);
    await expect(list.nth(0).locator(".file-size")).toHaveText(`${PNG.length} bytes`);
    // Any other file downloads under its name.
    const doc = list.nth(1).getByRole("link", { name: "Watering plan.txt" });
    await expect(doc).toHaveAttribute("download", "Watering plan.txt");
    await expect(doc).not.toHaveAttribute("target", /.*/);
    await expect(list.nth(1).locator("img")).toHaveCount(0);
    await axe(page);

    await list.nth(1).getByRole("button", { name: "Remove: Watering plan.txt" }).click();
    await expect(list).toHaveCount(1);
    await expect(page.locator(".toast")).toContainText("Removed Watering plan.txt.");
    expect(hub.files.get(txt.id)!.removed).toBe(true);
    await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
    await expect(list).toHaveCount(2);
    await expect(list.nth(1)).toContainText("Watering plan.txt");
    expect(hub.files.get(txt.id)!.removed).toBe(false);
  });

  test("refuses a file past 25 MB before sending it", async ({ page, hub: _hub }) => {
    const sent = watchUploads(page);
    const files = await openPlants(page);
    await files.getByLabel("Attach a file").setInputFiles({ name: "Vacation.mov", mimeType: "video/quicktime", buffer: Buffer.alloc(26 * 1024 * 1024 + 300 * 1024) });
    await expect(files.getByRole("alert")).toHaveText("Vacation.mov is 26.3 MB; a file can be at most 25 MB.");
    expect(sent).toEqual([]);
    await expect(files.locator(".file")).toHaveCount(0);
  });

  test("shows the hub's refusal when its files would pass 1 GB", async ({ page, hub }) => {
    hub.otherFileBytes = 1024 * 1024 * 1024 - 100;
    const files = await openPlants(page);
    await files.getByLabel("Attach a file").setInputFiles({ name: "fern.png", mimeType: "image/png", buffer: Buffer.alloc(30 * 1024) });
    await expect(files.getByRole("alert")).toHaveText("The hub's files would come to 1.00 GB; they can be at most 1 GB.");
    await expect(files.locator(".file")).toHaveCount(0);
  });

  test("shows the hub's error for any other failure", async ({ page, hub: _hub }) => {
    await page.route("**/api/items/*/attachments*", (route) =>
      route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "the disk is full" }) }),
    );
    const files = await openPlants(page);
    await files.getByLabel("Attach a file").setInputFiles({ name: "fern.png", mimeType: "image/png", buffer: PNG });
    await expect(files.getByRole("alert")).toHaveText("Could not attach fern.png: the disk is full");
  });

  test("Undo says so when the file is gone for good", async ({ page, hub }) => {
    const a = hub.attach(KEY, "fern.png", PNG);
    const files = await openPlants(page);
    await files.getByRole("button", { name: "Remove: fern.png" }).click();
    await expect(files.locator(".file")).toHaveCount(0);
    hub.purge(a.id);
    await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
    await expect(page.locator(".toast")).toContainText("Could not put fern.png back: the attachment was removed for good");
  });

  test("an agent's item has no Files section", async ({ page, hub: _hub }) => {
    await openApp(page);
    await row(page, "Order compost for the beds").locator(".row-body").click();
    await expect(page.locator("#detail .d-title")).toHaveText("Order compost for the beds");
    await expect(page.locator("#detail [data-attachments]")).toHaveCount(0);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test("offers the camera besides the file picker, and fits the screen", async ({ page, hub }) => {
    hub.attach(KEY, "A photo of the fern with a rather long name, taken in the morning.png", PNG);
    const files = await openPlants(page);
    const camera = files.getByLabel("Take a photo");
    await expect(camera).toHaveAttribute("capture", "environment");
    await expect(camera).toHaveAttribute("accept", "image/*");
    await camera.setInputFiles({ name: "image.jpg", mimeType: "image/jpeg", buffer: Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 0]) });
    await expect(files.locator(".file")).toHaveCount(2);
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await axe(page);
  });
});
