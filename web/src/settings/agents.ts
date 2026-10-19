// The instructions for agents, kept in Settings after setup.

import { agentMachines, instructionFile, instructionsHTML, pickMachine } from "../agents.ts";
import { esc } from "../html.ts";
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
    const machine = pickMachine(machines, chosen);
    const user = ctx.setup?.user || ctx.state?.user || "";
    const known = instructionFile(machines.find((b) => b.name === machine));
    const lead = known
      ? `Add this to ${esc(known.file)}, the file ${esc(known.harness)} reads on that machine, so agents there know how to raise tasks for you.`
      : "Add this to the instruction file your agents read, such as AGENTS.md or CLAUDE.md, so they know how to raise tasks for you.";
    return `<p class="muted">${lead}</p>
${instructionsHTML(user, machines, machine, "set-")}`;
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
