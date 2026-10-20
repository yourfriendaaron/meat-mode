import { test } from "node:test";
import assert from "node:assert/strict";
import { attachmentsHTML, capError, sizeText } from "../../src/attachments.ts";
import type { Item } from "../../src/types.ts";

const MB = 1024 * 1024;

test("a file's size reads in bytes, KB or MB", () => {
  assert.equal(sizeText(1), "1 byte");
  assert.equal(sizeText(820), "820 bytes");
  assert.equal(sizeText(14 * 1024 + 100), "14 KB");
  assert.equal(sizeText(3.24 * MB), "3.2 MB");
});

test("the caps refuse a file in the hub's words", () => {
  assert.equal(capError({ name: "Vacation.mov", size: 31.4 * MB }, 0, 0), "Vacation.mov is 31.4 MB; a file can be at most 25 MB.");
  assert.equal(capError({ name: "fern.png", size: 25 * MB }, 0, 0), "");
  assert.equal(capError({ name: "fern.png", size: 10 }, 20, 0), "An item can hold at most 20 files.");
  assert.equal(capError({ name: "fern.png", size: 10 }, 19, 0), "");
  assert.equal(capError({ name: "fern.png", size: 20 * MB }, 0, 1024 * MB), "The hub's files would come to 1.02 GB; they can be at most 1 GB.");
});

const item = (bridge: string, over: Partial<Item> = {}): Item => ({
  key: `${bridge}/personal/pe-1`,
  bridge,
  repo: "personal",
  id: "pe-1",
  title: "Water the plants",
  status: "open",
  priority: 2,
  createdAt: "2026-10-14T10:00:00Z",
  updatedAt: "2026-10-14T10:00:00Z",
  comments: [],
  ...over,
});

test("only an item of the hub's own lists has a Files section", () => {
  assert.equal(attachmentsHTML(item("home"), false), "");
  assert.match(attachmentsHTML(item("hub"), true), /data-attachments/);
  // A to-do not stored yet takes no file, and with none shows no section.
  assert.equal(attachmentsHTML(item("hub", { id: "pending-1" }), true), "");
});

test("a touch screen adds Take a photo and Choose a photo beside the file picker", () => {
  const html = (coarse: boolean): string => {
    const saved = globalThis.matchMedia;
    globalThis.matchMedia = ((q: string) => ({ matches: coarse && q === "(pointer: coarse)" })) as typeof matchMedia;
    try {
      return attachmentsHTML(item("hub"), true);
    } finally {
      globalThis.matchMedia = saved;
    }
  };
  const desktop = html(false);
  assert.match(desktop, /Attach a file<input type="file" class="sr" id="file-pick" multiple data-file-pick>/);
  assert.doesNotMatch(desktop, /Take a photo|Choose a photo/);
  const phone = html(true);
  assert.match(phone, /Take a photo<input type="file" class="sr" id="file-camera" accept="image\/\*" capture="environment" data-file-pick>/);
  // Choose a photo has no capture, so it opens the gallery, not the camera.
  assert.match(phone, /Choose a photo<input type="file" class="sr" id="file-photo" accept="image\/\*" multiple data-file-pick>/);
});

test("a file's name is escaped and only images get a thumbnail", () => {
  const html = attachmentsHTML(
    item("hub", {
      attachments: [
        { id: "a".repeat(32), name: '<b>"x".png', type: "image/png", size: 10, sha256: "", createdAt: "" },
        { id: "b".repeat(32), name: "page.svg", type: "image/svg+xml", size: 10, sha256: "", createdAt: "" },
      ],
    }),
    true,
  );
  assert.ok(!html.includes("<b>"));
  assert.match(html, /&lt;b&gt;&quot;x&quot;\.png/);
  assert.equal(html.match(/<img /g)?.length, 1);
  assert.match(html, /download="page.svg"/);
});
