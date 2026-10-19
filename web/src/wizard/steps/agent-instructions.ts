// Agents, part three: text for the agents' instruction file, saying how to
// raise a task for the user and how to add to their personal list.

import { agentMachines, instructionFile, instructionsHTML, pickMachine } from "../../agents.ts";
import { esc } from "../../html.ts";
import { copyText } from "../../clipboard.ts";
import { ORDER, registerStep } from "../registry.ts";

/** The machine chosen when there are several. */
let chosen = "";

registerStep({
  id: "agent-instructions",
  title: "Tell your agents",
  part: "hub",
  order: ORDER.agents + 2,
  settings: "s-agents",
  applies: (ctx) => agentMachines(ctx.state?.bridges).length > 0,
  render(ctx) {
    const machines = agentMachines(ctx.state?.bridges);
    const machine = pickMachine(machines, chosen);
    const user = ctx.setup?.user || ctx.state?.user || "";
    const known = instructionFile(machines.find((b) => b.name === machine));
    const where = known
      ? `<code>${esc(known.file)}</code>, the file ${esc(known.harness)} reads on that machine`
      : "the instruction file your agents read on that machine, such as <code>AGENTS.md</code> or <code>CLAUDE.md</code>";
    return `<p>Agents raise tasks for you only when they know how. Add this to ${where}.</p>
<p class="muted small">It holds no address or token: <code>meat-mode add</code> reads them from the machine's bridge.</p>
${instructionsHTML(user, machines, machine, "wiz-")}`;
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
    document.getElementById("wiz-agents-machine")?.focus();
  },
});
