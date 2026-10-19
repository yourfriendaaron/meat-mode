import { ApiError, errorText, sendFile } from "../../api.ts";
import { esc } from "../../html.ts";
import { ORDER, registerStep } from "../registry.ts";

/** POST /api/restore's answer (docs/api.md, Export and restore). */
export interface RestoreResult {
  items: number;
  actions: number;
  reminders: number;
  cancelled: number;
  machines: string[];
  calendarFeed: boolean;
  /** Files loaded onto the restored items. */
  attachments: number;
}

/** What the restore loaded, once it has; the step then stays in the run although the hub is no longer new. */
let loaded: RestoreResult | null = null;
let error = "";
/** A file is being sent; Continue and a second file wait for it. */
let busy = false;

/**
 * Sends the file as it was downloaded, the zip or an older hub's JSON: the
 * hub tells them apart by their first bytes. No timeout: a zip of every
 * attachment can take many minutes to send and load.
 */
function postRestore(file: Blob): Promise<RestoreResult> {
  return sendFile<RestoreResult>("/api/restore", file, 0);
}

function failure(e: unknown): string {
  // A 409 says the hub already has data; it reads plainly as it is.
  if (e instanceof ApiError && e.status === 409) return e.message;
  return errorText(e, { other: (m) => `Not restored. ${m}` });
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

function resultHTML(r: RestoreResult): string {
  const files = r.attachments ? ` with ${count(r.attachments, "file", "files")}` : "";
  const loadedLine = `Restored ${count(r.items, "to-do", "to-dos")}${files}, ${count(r.actions, "change", "changes")} in the history and ${count(r.reminders, "reminder", "reminders")} already sent.`;
  const cancelled = r.cancelled
    ? `<p class="muted">${esc(`${count(r.cancelled, "change was", "changes were")} still waiting for a machine and will not be sent.`)}</p>`
    : "";
  const todo: string[] = [];
  if (r.machines.length) {
    todo.push(
      `Pair each machine again, under the same name, so it gets back the to-dos its agents added: ${r.machines.map((m) => `<code>${esc(m)}</code>`).join(", ")}.`,
    );
  }
  if (r.calendarFeed) todo.push("Turn the calendar feed on again in Settings, and give your calendars its new address.");
  todo.push("Turn notifications on, on each device.");
  return `<p role="status">${esc(loadedLine)}</p>${cancelled}
<h3>What to do again</h3>
<ul class="restore-todo">${todo.map((t) => `<li>${t}</li>`).join("")}</ul>
<p class="muted">Continue goes on with setup.</p>`;
}

registerStep({
  id: "restore",
  title: "Restore from an export",
  part: "hub",
  order: ORDER.restore,
  // Only a fresh hub takes a restore.
  applies: (ctx) => loaded !== null || ctx.setup?.status === "new",
  done: () => loaded !== null,
  start() {
    loaded = null;
    error = "";
  },
  render() {
    if (loaded) return resultHTML(loaded);
    return `<p>Moving from another hub? Choose the export file you downloaded from its Settings. It brings back your own list, your projects and their files, the history and your settings.</p>
<p class="muted">Restore works only on a fresh hub. Skip this step to start with an empty one.</p>
<p><label for="restore-file">Export file</label><br><input id="restore-file" type="file" accept=".zip,.json,application/zip,application/json" data-restore-file${busy ? " disabled" : ""}></p>
${busy ? '<p class="muted" role="status">Restoring…</p>' : ""}${error ? `<p class="row-error" role="alert">${esc(error)}</p>` : ""}`;
  },
  next() {
    return !busy;
  },
  async change(el, ctx) {
    if (!(el instanceof HTMLInputElement) || el.dataset.restoreFile === undefined) return;
    const file = el.files?.[0];
    if (!file || busy) return;
    // The redraw keeps what inputs hold, which a file input refuses; the
    // same file can then be chosen again after a refusal.
    el.value = "";
    busy = true;
    error = "";
    ctx.render();
    try {
      loaded = await postRestore(file);
    } catch (e) {
      error = failure(e);
    } finally {
      busy = false;
    }
    if (loaded) await ctx.refresh();
    else ctx.render();
  },
});
