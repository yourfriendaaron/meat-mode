// The export (docs/api.md, Export and restore): one file with everything
// the hub alone holds, to keep as a backup.

import type { AppContext } from "../app-context.ts";
import { ApiError, errorText } from "../api.ts";
import { registerSection } from "./registry.ts";

let busy = false;

/** The file name the hub gave in Content-Disposition, or a plain one. */
export function exportFileName(disposition: string | null): string {
  return /filename="([^"]+)"/.exec(disposition ?? "")?.[1] ?? "meat-mode-export.json";
}

/** Fetches the export and hands it to the browser to save. */
async function download(ctx: AppContext) {
  if (busy) return;
  busy = true;
  ctx.render();
  try {
    const res = await fetch("/api/export", { signal: AbortSignal.timeout(60000) });
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`;
      try {
        const j = await res.json();
        if (j && typeof j.error === "string") msg = j.error;
      } catch {
        // keep the status line
      }
      throw new ApiError(res.status, msg);
    }
    const name = exportFileName(res.headers.get("Content-Disposition"));
    const url = URL.createObjectURL(await res.blob());
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10000);
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
    return `<p>Download one file with your personal list, your projects, the history of changes and your settings. Keep it somewhere safe: a new hub can be set up from it.</p>
<button class="btn" id="data-export" data-act="data-export"${busy ? " disabled" : ""}>${busy ? "Downloading" : "Download your data"}</button>
<p class="muted small">The file leaves out the lists from your machines, which come back when each machine is paired again, and every password, key and private link.</p>`;
  },
  act(act, _el, ctx) {
    if (act === "data-export") return download(ctx);
  },
});
