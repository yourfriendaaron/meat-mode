// The export (docs/api.md, Export and restore): one file with everything
// the hub alone holds, to keep as a backup.

import type { AppContext } from "../app-context.ts";
import { call, errorText } from "../api.ts";
import { registerSection } from "./registry.ts";

/** The name the browser saves the export under when the hub's reply names none. */
export const EXPORT_FALLBACK_NAME = "meat-mode-export.zip";

let busy = false;

/**
 * Hands the export to the browser as a same-origin download link. The zip
 * holds every attachment, up to a gigabyte, so the browser streams it to
 * disk under the name the hub gives; read through fetch, it would sit in
 * memory and outlast any timeout. A failure of the export itself then shows
 * in the browser's downloads, so a cheap call first catches an unreachable
 * hub or an ended session, which the app can name.
 */
async function download(ctx: AppContext) {
  if (busy) return;
  busy = true;
  ctx.render();
  try {
    await call("GET", "/api/settings");
    const a = document.createElement("a");
    a.href = "/api/export";
    a.download = EXPORT_FALLBACK_NAME;
    document.body.append(a);
    a.click();
    a.remove();
  } catch (e) {
    ctx.toast(`Could not download your data: ${errorText(e)}`);
  }
  busy = false;
  ctx.render();
  document.getElementById("data-export")?.focus();
}

registerSection({
  id: "data",
  title: "Your data",
  order: 47,
  render() {
    return `<p>Download one file with your personal list, your projects and their files, the history of changes and your settings. Keep it somewhere safe: a new hub can be set up from it.</p>
<button class="btn" id="data-export" data-act="data-export"${busy ? " disabled" : ""}>${busy ? "Downloading" : "Download your data"}</button>
<p class="muted small">The file leaves out the lists from your machines, which come back when each machine is paired again, and every password, key and private link.</p>`;
  },
  act(act, _el, ctx) {
    if (act === "data-export") return download(ctx);
  },
});
