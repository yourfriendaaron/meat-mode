// Agents, part one: whether the user has agents at all, then adding a machine,
// installing and pairing its bridge, and waiting for its first hello.

import type { AppContext } from "../../app-context.ts";
import { addMachineError, agentMachines, installCommand, pairCommand, PLATFORMS, reauthFieldHTML, type Platform } from "../../agents.ts";
import { copyText } from "../../clipboard.ts";
import { api } from "../../api.ts";
import { esc } from "../../html.ts";
import type { NewBridgeReply } from "../../types.ts";
import { ORDER, registerStep } from "../registry.ts";

/** The user answered yes to having agents. */
let wanted = false;
/** The machine added in this step, with its token, shown once and dropped on leaving. */
let pairing: NewBridgeReply | null = null;
let platform: Platform = "linux-amd64";
let error = "";
/** The hub asked for the password again before it adds a machine. */
let reauth = false;
let busy = false;
let poll: ReturnType<typeof setInterval> | undefined;

/** How often to ask the hub whether the new machine said hello, in case live updates do not arrive. */
const POLL_MS = 3000;

function onScreen(): boolean {
  return !!document.querySelector('[data-step="agents"]');
}

function connected(ctx: AppContext, name: string): boolean {
  return !!agentMachines(ctx.state?.bridges).find((b) => b.name === name)?.lastSeen;
}

function stopPolling() {
  clearInterval(poll);
  poll = undefined;
}

/** Drops the one-time token and everything tied to it, so a revisit never draws it again. */
function forget() {
  pairing = null;
  error = "";
  reauth = false;
  stopPolling();
}

function waitForHello(ctx: AppContext, name: string) {
  stopPolling();
  poll = setInterval(() => {
    if (!onScreen() || connected(ctx, name)) stopPolling();
    else void ctx.refresh();
  }, POLL_MS);
}

function machinesHTML(ctx: AppContext): string {
  const machines = agentMachines(ctx.state?.bridges);
  if (!machines.length) return "";
  const lost = !pairing && machines.some((b) => !b.lastSeen);
  return `<ul class="machines agents-machines">${machines
    .map(
      (b) =>
        `<li class="machine"><span class="m-name">${esc(b.name)}</span><span class="${b.lastSeen ? "agents-on" : "muted"}">${b.lastSeen ? "Connected" : "Not connected yet"}</span></li>`,
    )
    .join("")}</ul>${
    lost
      ? '<p class="muted small">A pairing token is shown only once. If a machine never connected and its command is lost, revoke it in Settings and add it again.</p>'
      : ""
  }`;
}

function pairingHTML(ctx: AppContext, p: NewBridgeReply): string {
  const on = connected(ctx, p.name);
  const options = PLATFORMS.map((o) => `<option value="${o.id}"${o.id === platform ? " selected" : ""}>${esc(o.label)}</option>`).join("");
  return `<div class="token-box agents-pair" id="agents-pair" tabindex="-1">
<p>Run these on <strong>${esc(p.name)}</strong>, the machine your agents work on. It needs bd 1.3.0 or later.</p>
<h3>1. Install Meat Mode</h3>
<div class="agents-pick"><label for="agents-platform">Platform</label><select id="agents-platform" data-agents-platform>${options}</select></div>
<code class="cmd" id="agents-install">${esc(installCommand(platform))}</code>
<button class="btn" data-act="agents-copy" data-copy="agents-install">Copy</button>
<h3>2. Pair it with this hub</h3>
<p class="muted small">The token is shown only once.</p>
<code class="cmd" id="agents-pair-cmd">${esc(pairCommand(location.origin, p.token))}</code>
<button class="btn" data-act="agents-copy" data-copy="agents-pair-cmd">Copy</button>
<p class="agents-status${on ? " agents-on" : ""}" role="status">${on ? `Connected. ${esc(p.name)} is talking to this hub.` : `Waiting for ${esc(p.name)} to connect…`}</p>
</div>`;
}

function formHTML(again: boolean): string {
  return `<form class="add-machine" data-form="agents-pair"><label for="agents-name">${again ? "Add another machine" : "Machine name"}</label><div class="pick-row"><input id="agents-name" name="name" required pattern="[a-z0-9-]{1,32}" placeholder="laptop" autocomplete="off" aria-describedby="agents-name-hint"><button class="btn${again ? "" : " btn-primary"}" type="submit"${busy ? " disabled" : ""}>Add machine</button></div><p class="muted small" id="agents-name-hint">Lowercase letters, digits and dashes, such as laptop or desk.</p>${
    reauth ? reauthFieldHTML("agents") : ""
  }${error ? `<p class="row-error" role="alert">${esc(error)}</p>` : ""}</form>`;
}

registerStep({
  id: "agents",
  title: "Agent machines",
  part: "hub",
  order: ORDER.agents,
  settings: "s-mach",
  done: (ctx) => agentMachines(ctx.state?.bridges).some((b) => !!b.lastSeen),
  render(ctx) {
    const machines = agentMachines(ctx.state?.bridges);
    if (!wanted && !machines.length && !pairing) {
      return `<p><strong>Do you use AI agents with beads?</strong></p>
<p>Meat Mode can show the tasks your agents raise for you in beads repos. A small bridge on the machine they work on connects it to this hub.</p>
<div class="btn-row"><button class="btn" data-act="agents-yes">Yes, add a machine</button><button class="btn" data-act="wizard-skip">No</button></div>`;
    }
    const intro = machines.length
      ? ""
      : "<p>Name the machine your agents work on. You can add more later in Settings.</p>";
    return `${intro}${machinesHTML(ctx)}${pairing ? pairingHTML(ctx, pairing) : formHTML(machines.length > 0)}`;
  },
  start() {
    forget();
    wanted = false;
  },
  leave: forget,
  next(ctx) {
    if (!agentMachines(ctx.state?.bridges).length) {
      // Continue on the question means yes; on the empty form it asks for a name.
      if (wanted) error = "Add a machine, or skip this step.";
      wanted = true;
      ctx.render();
      document.getElementById("agents-name")?.focus();
      return false;
    }
    forget();
    return true;
  },
  async act(act, el, ctx) {
    if (act === "agents-yes") {
      wanted = true;
      ctx.render();
      document.getElementById("agents-name")?.focus();
    } else if (act === "agents-copy") {
      await copyText(el.dataset.copy ?? "");
      ctx.toast("Copied");
    }
  },
  async submit(form, ctx) {
    const name = (form.querySelector<HTMLInputElement>("input[name=name]")?.value ?? "").trim();
    if (!/^[a-z0-9-]{1,32}$/.test(name)) {
      error = "Use 1 to 32 lowercase letters, digits or dashes.";
      ctx.render();
      document.getElementById("agents-name")?.focus();
      return;
    }
    if (busy) return;
    const pw = form.querySelector<HTMLInputElement>("input[name=password]");
    const password = reauth && pw ? pw.value : undefined;
    busy = true;
    try {
      pairing = await api.addBridge(name, password);
      error = "";
      reauth = false;
      waitForHello(ctx, name);
    } catch (e) {
      ({ error, reauth } = addMachineError(e, password !== undefined));
    }
    busy = false;
    await ctx.refresh();
    const field = reauth ? document.querySelector<HTMLInputElement>("#agents-password") : null;
    if (field) {
      field.value = "";
      field.focus();
    } else document.getElementById(pairing ? "agents-pair" : "agents-name")?.focus();
  },
  change(el, ctx) {
    if (!(el instanceof HTMLSelectElement) || !("agentsPlatform" in el.dataset)) return;
    platform = el.value as Platform;
    ctx.render();
    document.getElementById("agents-platform")?.focus();
  },
});
