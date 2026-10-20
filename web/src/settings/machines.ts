// The Agent machines section: the paired machines with their repos, their
// limits on agent runs, Revoke, and the form that adds one and shows its
// pairing command once.

import {
  addMachineError,
  harnessLinesHTML,
  LIMIT_NAMES,
  limitsBoxHTML,
  limitsChange,
  machineReposHTML,
  pairCommand,
  reauthFieldHTML,
  runLimits,
  trackersText,
} from "../agents.ts";
import type { AppContext } from "../app-context.ts";
import { api, errorText, newId } from "../api.ts";
import { copyText } from "../clipboard.ts";
import { fmtAgo } from "../dates.ts";
import { esc } from "../html.ts";
import type { LimitName, LimitsChange, NewBridgeReply } from "../types.ts";
import { registerSection } from "./registry.ts";

const m = {
  /** The machine just added, whose pairing command shows until Done or leaving Settings. */
  added: null as NewBridgeReply | null,
  copied: false,
  error: "",
  /** The hub asked for the password again before it adds a machine. */
  reauth: false,
  adding: false,
  /** The machine being asked about before it is revoked. */
  revoking: null as string | null,
  /** The machine whose limits form is open, what was typed in it, and why the hub refused it. */
  limits: null as string | null,
  drafts: {} as Partial<Record<LimitName, string>>,
  limitsError: "",
  limitsSaving: false,
};

/** A hub message as a sentence. */
function sentence(t: string): string {
  const s = t.trim();
  return s ? `${s[0].toUpperCase()}${s.slice(1)}${/[.!?]$/.test(s) ? "" : "."}` : s;
}

function closeLimits() {
  m.limits = null;
  m.drafts = {};
  m.limitsError = "";
}

/** Posts a limits change for a machine; the bridge applies it and reports the limits back. */
async function postLimits(bridge: string, change: LimitsChange, ctx: AppContext): Promise<boolean> {
  try {
    await api.postAction({ id: newId(), kind: "limits", bridge, payload: { limits: change } });
    return true;
  } catch (e) {
    m.limitsError = sentence(errorText(e));
    return false;
  } finally {
    await ctx.refresh();
  }
}

async function saveLimits(form: HTMLFormElement, ctx: AppContext) {
  const bridge = form.dataset.bridge ?? "";
  const b = (ctx.state?.bridges ?? []).find((x) => x.name === bridge);
  const l = b && runLimits(b);
  if (!l || m.limitsSaving) return;
  const typed: Partial<Record<LimitName, string>> = {};
  const shown: Partial<Record<LimitName, string>> = {};
  for (const n of LIMIT_NAMES) {
    const f = form.querySelector<HTMLInputElement>(`input[name="${n}"]`);
    if (!f) continue;
    typed[n] = f.value;
    shown[n] = f.dataset.shown ?? "";
  }
  m.drafts = typed;
  const { change, error } = limitsChange(typed, shown, l.max, bridge);
  if (error) {
    m.limitsError = error;
    ctx.render();
    return;
  }
  if (!Object.keys(change).length) {
    closeLimits();
    ctx.render();
    return;
  }
  m.limitsSaving = true;
  m.limitsError = "";
  ctx.render();
  const ok = await postLimits(bridge, change, ctx);
  m.limitsSaving = false;
  if (ok) closeLimits();
  ctx.render();
}

async function add(form: HTMLFormElement, ctx: AppContext) {
  const input = form.querySelector<HTMLInputElement>("input[name=name]")!;
  const name = input.value.trim();
  if (!/^[a-z0-9-]{1,32}$/.test(name)) {
    m.error = "Use 1 to 32 lowercase letters, digits or dashes.";
    ctx.render();
    return;
  }
  if (m.adding) return;
  const pw = form.querySelector<HTMLInputElement>("input[name=password]");
  const password = m.reauth && pw ? pw.value : undefined;
  m.adding = true;
  try {
    m.added = await api.addBridge(name, password);
    m.error = "";
    m.reauth = false;
    m.copied = false;
    // The name is used; keep it out of the form, or a second tap would ask for the same name again.
    input.value = "";
  } catch (e) {
    ({ error: m.error, reauth: m.reauth } = addMachineError(e, password !== undefined));
  }
  m.adding = false;
  await ctx.refresh();
  if (m.reauth) {
    const field = document.querySelector<HTMLInputElement>("#bridge-password");
    if (field) {
      field.value = "";
      field.focus();
    }
  }
}

async function revoke(name: string, ctx: AppContext) {
  try {
    await api.revokeBridge(name);
    if (m.added?.name === name) m.added = null;
  } catch (e) {
    ctx.toast(`Could not revoke: ${errorText(e)}`);
  }
  m.revoking = null;
  await ctx.refresh();
}

registerSection({
  id: "mach",
  title: "Agent machines",
  order: 40,
  render(ctx) {
    const n = ctx.now();
    const bridges = ctx.state?.bridges ?? [];
    const repos = ctx.state?.repos ?? [];
    const nb = m.added;
    const machines = bridges.length
      ? `<ul class="machines">${bridges
          .map((b) => {
            const seen = b.lastSeen ? `Last seen ${fmtAgo(new Date(b.lastSeen), n)}` : "Never seen";
            const trackers = trackersText(b);
            const actions =
              m.revoking === b.name
                ? `<span class="confirm">Revoke ${esc(b.name)}?</span><button class="btn btn-small btn-danger" data-act="revoke-confirm" data-name="${esc(b.name)}">Revoke</button><button class="btn btn-small" data-act="revoke-cancel">Cancel</button>`
                : `<button class="btn btn-small" data-act="revoke" data-name="${esc(b.name)}">Revoke</button>`;
            return `<li class="machine"><div><div class="m-name">${esc(b.name)}</div><div class="muted small">${esc(seen)}${b.version ? ` · ${esc(b.version)}` : ""}</div>${trackers ? `<div class="muted small m-trackers">${esc(trackers)}</div>` : ""}${harnessLinesHTML(b)}</div><div class="m-actions">${actions}</div>${limitsBoxHTML(b, { open: m.limits === b.name, drafts: m.drafts, error: m.limitsError, saving: m.limitsSaving })}${machineReposHTML(repos, b.name)}</li>`;
          })
          .join("")}</ul>`
      : '<p class="muted">No machines yet.</p>';
    return `${machines}
<form class="add-machine" data-form="bridge"><label for="bridge-name">Machine name</label><div class="pick-row"><input id="bridge-name" name="name" required pattern="[a-z0-9-]{1,32}" placeholder="laptop" autocomplete="off"><button class="btn btn-primary" type="submit"${m.adding ? " disabled" : ""}>Add machine</button></div><p class="muted small">Lowercase letters, digits and dashes.</p>${
      m.reauth ? reauthFieldHTML("bridge") : ""
    }${m.error ? `<p class="row-error" role="alert">${esc(m.error)}</p>` : ""}</form>
${
  nb
    ? `<div class="token-box" role="status"><p>Run this on <strong>${esc(nb.name)}</strong>. The token is shown only once.</p><code class="cmd" id="pair-cmd">${esc(pairCommand(location.origin, nb.token))}</code><button class="btn" data-act="copy">${m.copied ? "Copied" : "Copy"}</button><button class="btn" data-act="token-done">Done</button></div>`
    : ""
}`;
  },
  // The pairing token is shown once; leaving Settings ends that showing.
  leave() {
    m.added = null;
    m.copied = false;
    m.error = "";
    m.reauth = false;
    m.revoking = null;
    closeLimits();
  },
  submit(form, ctx) {
    if (form.dataset.form === "bridge") return add(form, ctx);
    if (form.dataset.form === "limits") return saveLimits(form, ctx);
  },
  change(el) {
    // What was typed in the limits form outlives a redraw.
    const name = el.getAttribute("name") as LimitName | null;
    if (el instanceof HTMLInputElement && el.closest("[data-form=limits]") && name && LIMIT_NAMES.includes(name)) m.drafts[name] = el.value;
  },
  async act(act, el, ctx) {
    switch (act) {
      case "revoke":
        m.revoking = el.dataset.name ?? null;
        ctx.render();
        break;
      case "revoke-cancel":
        m.revoking = null;
        ctx.render();
        break;
      case "revoke-confirm":
        await revoke(el.dataset.name ?? "", ctx);
        break;
      case "copy":
        await copyText("pair-cmd");
        m.copied = true;
        ctx.render();
        break;
      case "limits-edit":
        closeLimits();
        m.limits = el.dataset.bridge ?? null;
        ctx.render();
        document.querySelector<HTMLInputElement>(`[data-form=limits] input`)?.focus();
        break;
      case "limits-cancel":
        closeLimits();
        ctx.render();
        break;
      case "limit-reset": {
        const name = el.dataset.limit as LimitName;
        if (!LIMIT_NAMES.includes(name)) break;
        delete m.drafts[name];
        m.limitsError = "";
        await postLimits(el.dataset.bridge ?? "", { reset: [name] }, ctx);
        ctx.render();
        break;
      }
      case "token-done":
        m.added = null;
        m.copied = false;
        ctx.render();
        break;
    }
  },
});
