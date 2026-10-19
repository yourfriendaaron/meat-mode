// The Agents section: what agents are working on now, with Stop all, and the
// rules every run keeps.

import { agentMachines, HARNESS_PICK, harnessLinesHTML } from "../agents.ts";
import { newId } from "../api.ts";
import { esc } from "../html.ts";
import { ICON } from "../icons.ts";
import { agentName, workingItems } from "../runs.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "runs",
  title: "Agents",
  order: 42,
  applies: (ctx) => agentMachines(ctx.state?.bridges).length > 0,
  render(ctx) {
    const working = workingItems(ctx.state?.items ?? []);
    const now = working.length
      ? `<div class="runs-now" data-run="working"><span class="run-fx" aria-hidden="true"></span><div class="runs-text"><b>${working.length === 1 ? "1 agent working" : `${working.length} agents working`}</b><ul class="runs-list">${working
          .map((i) => `<li class="run-line" data-run="working"><b>${esc(agentName(i))}</b> · ${esc(i.title)}</li>`)
          .join("")}</ul></div><button class="btn btn-small btn-danger" data-act="stop-all">${ICON.stop}Stop all</button></div>`
      : '<p class="muted">No agent is working now.</p>';
    const machines = agentMachines(ctx.state?.bridges).filter((b) => b.lastSeen);
    const harnesses = machines.length
      ? `<ul class="runs-machines" aria-label="Harness on each machine">${machines.map((b) => `<li><div class="m-name">${esc(b.name)}</div>${harnessLinesHTML(b)}</li>`).join("")}</ul><p class="muted small">${esc(HARNESS_PICK)}</p>`
      : "";
    return `${now}<p class="muted small">Agents start only from your pink taps, one per task, and each run keeps the limits its machine sets.</p>${harnesses}`;
  },
  async act(act, _el, ctx) {
    if (act !== "stop-all") return;
    const working = workingItems(ctx.state?.items ?? []);
    const h = await ctx.perform(working.map((i) => ({ id: newId(), kind: "stop", key: i.key, payload: {} })));
    const failed = working.length - h.posted.length;
    ctx.toast(failed ? `Could not stop ${failed} of ${working.length}` : working.length === 1 ? "Stopped 1 agent" : `Stopped ${working.length} agents`);
    await ctx.refresh();
  },
});
