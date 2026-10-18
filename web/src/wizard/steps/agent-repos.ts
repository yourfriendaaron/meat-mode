// Agents, part two: which of a machine's beads repos sync, and where agents
// may work. The switches are the ones in Settings; main.ts handles their
// "sync-switch" and "agents-switch" clicks.

import { agentMachines, machineReposHTML } from "../../agents.ts";
import { esc } from "../../html.ts";
import { HUB_BRIDGE } from "../../model.ts";
import { ORDER, registerStep } from "../registry.ts";

registerStep({
  id: "agent-repos",
  title: "Choose repos",
  part: "hub",
  order: ORDER.agents + 1,
  settings: "s-mach",
  applies: (ctx) => agentMachines(ctx.state?.bridges).length > 0,
  done: (ctx) => (ctx.state?.repos ?? []).some((r) => r.bridge !== HUB_BRIDGE && r.optedIn),
  render(ctx) {
    const repos = ctx.state?.repos ?? [];
    const machines = agentMachines(ctx.state?.bridges)
      .map((b) => {
        const body = b.lastSeen
          ? machineReposHTML(repos, b.name, "wiz-sync-")
          : `<p class="muted small m-repos">Waiting for ${esc(b.name)} to connect. Its repos appear here when it does.</p>`;
        return `<li class="machine"><h3 class="m-name">${esc(b.name)}</h3>${body}</li>`;
      })
      .join("");
    return `<p>Switch on the repos whose tasks you want here. Each machine lists the beads repos it found in its home folder.</p>
<p class="muted">Only beads labelled <code>human</code> appear, so your agents' own work stays out of your list.</p>
<p class="muted">Under each repo, Agents can work here lets an agent start on one of its tasks when you tap a pink button. It stays off until you switch it on.</p>
<ul class="machines">${machines}</ul>`;
  },
});
