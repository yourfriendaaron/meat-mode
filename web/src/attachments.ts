// Files attached to an item of the hub's own lists (docs/api.md,
// Attachments): the detail view's section, and its events through ext.ts.

import type { AppContext, Handlers } from "./app-context.ts";
import { ApiError, api, attachmentURL, errorText } from "./api.ts";
import { esc } from "./html.ts";
import { ICON } from "./icons.ts";
import { isPersonal } from "./model.ts";
import { MAX_ATTACHMENT_BYTES, MAX_ATTACHMENTS_BYTES, MAX_ATTACHMENTS_PER_ITEM } from "./types.ts";
import type { Attachment, Item } from "./types.ts";

const MB = 1024 * 1024;
const GB = 1024 * MB;

/** Types the hub serves inline, so the app shows them as thumbnails and opens them in a tab. */
const INLINE_TYPES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

const CLIP =
  '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M21 11.5l-8.6 8.6a5.5 5.5 0 0 1-7.8-7.8l8.6-8.6a3.7 3.7 0 0 1 5.2 5.2l-8.6 8.6a1.8 1.8 0 0 1-2.6-2.6l7.9-7.9"/></svg>';
const CAMERA =
  '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 8a2 2 0 0 1 2-2h2.5l1.5-2h6l1.5 2H19a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><circle cx="12" cy="13" r="3.5"/></svg>';
const PHOTO =
  '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="9.5" r="1.5"/><path d="M21 16l-5-5-9 9"/></svg>';
const FILE =
  '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></svg>';

/** What the section says while it sends files or after one failed, for one item. */
let status: { key: string; sending: string; error: string } | null = null;

/** A file's size as the list shows it: "820 bytes", "14 KB", "3.2 MB". */
export function sizeText(bytes: number): string {
  if (bytes < 1024) return `${bytes} ${bytes === 1 ? "byte" : "bytes"}`;
  if (bytes < MB) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / MB).toFixed(1)} MB`;
}

/**
 * Why the hub would refuse this file, in the hub's own words, or "".
 * `count` is how many files the item holds and `total` the bytes of every
 * file in the app's state, both counting files already sent in this batch.
 * The hub also counts removed files it has not purged, which the app does
 * not see, so it may still refuse a file this lets through.
 */
export function capError(file: { name: string; size: number }, count: number, total: number): string {
  if (file.size > MAX_ATTACHMENT_BYTES) {
    return `${file.name} is ${(file.size / MB).toFixed(1)} MB; a file can be at most ${MAX_ATTACHMENT_BYTES / MB} MB.`;
  }
  if (count >= MAX_ATTACHMENTS_PER_ITEM) return `An item can hold at most ${MAX_ATTACHMENTS_PER_ITEM} files.`;
  if (total + file.size > MAX_ATTACHMENTS_BYTES) {
    return `The hub's files would come to ${((total + file.size) / GB).toFixed(2)} GB; they can be at most ${MAX_ATTACHMENTS_BYTES / GB} GB.`;
  }
  return "";
}

/** The bytes of every attachment the app knows of. */
function totalBytes(items: Item[]): number {
  let n = 0;
  for (const it of items) for (const a of it.attachments ?? []) n += a.size;
  return n;
}

/** A phone or tablet, which gets camera and photo buttons besides the file picker. */
function coarse(): boolean {
  return typeof matchMedia === "function" && matchMedia("(pointer: coarse)").matches;
}

function fileHTML(a: Attachment, editable: boolean): string {
  const url = esc(attachmentURL(a.id));
  const inline = INLINE_TYPES.has(a.type);
  // An image opens in a tab; any other file downloads under its name, as the hub serves it.
  const open = inline ? ' target="_blank" rel="noopener noreferrer"' : ` download="${esc(a.name)}"`;
  const thumb = inline ? `<img class="file-thumb" src="${url}" alt="" loading="lazy">` : `<span class="file-thumb file-icon">${FILE}</span>`;
  const remove = editable
    ? `<button class="link-btn file-remove" data-act="file-remove" data-id="${esc(a.id)}" aria-label="${esc(`Remove: ${a.name}`)}" title="Remove">${ICON.close}</button>`
    : "";
  return `<li class="file"><a class="file-open" href="${url}"${open}>${thumb}<span class="file-name">${esc(a.name)}</span></a><span class="file-size">${esc(sizeText(a.size))}</span>${remove}</li>`;
}

/** The detail view's Files section, or "" for an item that takes none. */
export function attachmentsHTML(item: Item, editable: boolean): string {
  if (!isPersonal(item)) return "";
  const files = item.attachments ?? [];
  // A to-do the hub has not stored yet has no key to attach to.
  const canAdd = editable && !item.id.startsWith("pending-");
  if (!files.length && !canAdd) return "";
  const st = status?.key === item.key ? status : null;
  const off = st?.sending ? " disabled" : "";
  const add = canAdd
    ? `<div class="files-add"><label class="btn file-pick">${CLIP}Attach a file<input type="file" class="sr" id="file-pick" multiple data-file-pick${off}></label>${
        coarse()
          ? `<label class="btn file-pick">${CAMERA}Take a photo<input type="file" class="sr" id="file-camera" accept="image/*" capture="environment" data-file-pick${off}></label>` +
            `<label class="btn file-pick">${PHOTO}Choose a photo<input type="file" class="sr" id="file-photo" accept="image/*" multiple data-file-pick${off}></label>`
          : ""
      }</div>`
    : "";
  const list = files.length
    ? `<ul class="files">${files.map((a) => fileHTML(a, editable)).join("")}</ul>`
    : '<p class="muted small">No files.</p>';
  const note = st?.sending
    ? `<p class="muted small" role="status">${esc(`Attaching ${st.sending}…`)}</p>`
    : st?.error
      ? `<p class="row-error" role="alert">${esc(st.error)}</p>`
      : "";
  return `<section class="d-section" data-attachments data-key="${esc(item.key)}" aria-labelledby="d-files"><h3 id="d-files">Files</h3>${list}${note}${add}</section>`;
}

function itemOf(ctx: AppContext, pred: (i: Item) => boolean): Item | undefined {
  return ctx.state?.items?.find(pred);
}

/** Sends the chosen files one by one; the first refusal stops the rest. */
async function attach(item: Item, files: File[], ctx: AppContext) {
  const all = ctx.state?.items ?? [];
  let count = item.attachments?.length ?? 0;
  let total = totalBytes(all);
  status = { key: item.key, sending: "", error: "" };
  for (const file of files) {
    const why = capError(file, count, total);
    if (why) {
      status.error = why;
      break;
    }
    status.sending = file.name;
    ctx.render();
    try {
      await api.attach(item.key, file);
      count++;
      total += file.size;
    } catch (e) {
      // A cap's refusal names the file and the cap itself.
      status.error = e instanceof ApiError && e.status === 413 ? e.message : errorText(e, { other: (m) => `Could not attach ${file.name}: ${m}` });
      break;
    } finally {
      status.sending = "";
    }
  }
  const failed = status.error !== "";
  if (!failed) status = null;
  await ctx.refresh();
  if (!failed) document.getElementById("file-pick")?.focus();
}

/** Removes a file, with Undo while the hub keeps it. */
async function remove(id: string, ctx: AppContext) {
  const item = itemOf(ctx, (i) => !!i.attachments?.some((a) => a.id === id));
  const name = item?.attachments?.find((a) => a.id === id)?.name ?? "the file";
  if (item && status?.key === item.key) status = null;
  try {
    await api.removeAttachment(id);
  } catch (e) {
    ctx.toast(`Could not remove ${name}: ${errorText(e)}`);
    return;
  }
  await ctx.refresh();
  document.getElementById("file-pick")?.focus();
  ctx.toast(`Removed ${name}.`, async () => {
    try {
      await api.restoreAttachment(id);
    } catch (e) {
      ctx.toast(`Could not put ${name} back: ${errorText(e)}`);
      return;
    }
    await ctx.refresh();
  });
}

/** Events inside [data-attachments]. */
export const attachmentHandlers: Handlers = {
  act(act, el, ctx) {
    if (act === "file-remove") return remove(el.dataset.id ?? "", ctx);
  },
  change(el, ctx) {
    if (!(el instanceof HTMLInputElement) || el.dataset.filePick === undefined) return;
    const files = [...(el.files ?? [])];
    // The redraw keeps what inputs hold, which a file input refuses.
    el.value = "";
    const key = el.closest<HTMLElement>("[data-attachments]")?.dataset.key;
    const item = key ? itemOf(ctx, (i) => i.key === key) : undefined;
    if (!item || !files.length || status?.sending) return;
    void attach(item, files, ctx);
  },
};

