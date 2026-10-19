// The instructions for agents, kept in Settings after setup.

import { agentMachines, instructionsHTML, pickMachine } from "../agents.ts";
import { copyText } from "../clipboard.ts";
import { registerSection } from "./registry.ts";

/** The machine chosen when there are several. */
let chosen = "";

registerSection({
  id: "agents",
  title: "Instructions for agents",
  order: 45,
  applies: (ctx) => agentMachines(ctx.state?.bridges).length > 0,
  render(ctx) {
    const machines = agentMachines(ctx.state?.bridges);
    const user = ctx.setup?.user || ctx.state?.user || "";
    return `<p class="muted">Add this to the instruction file your agents read, such as AGENTS.md or CLAUDE.md, so they know how to raise tasks for you.</p>
${instructionsHTML(user, machines, pickMachine(machines, chosen), "set-")}`;
  },
  async act(act, el, ctx) {
    if (act !== "agents-copy") return;
    await copyText(el.dataset.copy ?? "");
    ctx.toast("Copied");
  },
  change(el, ctx) {
    if (!(el instanceof HTMLSelectElement) || !("agentsMachine" in el.dataset)) return;
    chosen = el.value;
    ctx.render();
    document.getElementById("set-agents-machine")?.focus();
  },
});
