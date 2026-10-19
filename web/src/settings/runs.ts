// The Agents section: what agents are working on now, with Stop all, and the
// rules every run keeps.

import { agentMachines } from "../agents.ts";
import { newId } from "../api.ts";
import { esc } from "../html.ts";
import { agentName, workingItems } from "../runs.ts";
import { registerSection } from "./registry.ts";

const STOP_ICON = '<svg class="i-fill" viewBox="0 0 24 24" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>';

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
          .join("")}</ul></div><button class="btn btn-small btn-danger" data-act="stop-all">${STOP_ICON}Stop all</button></div>`
      : '<p class="muted">No agent is working now.</p>';
    return `${now}<p class="muted small">Agents start only from your pink taps, one per task, and each run has a time limit set on its machine.</p>`;
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
