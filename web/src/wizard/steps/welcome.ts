import { esc } from "../../html.ts";
import { ORDER, registerStep } from "../registry.ts";

registerStep({
  id: "welcome",
  title: "Welcome",
  part: null,
  order: ORDER.welcome,
  skippable: false,
  continueLabel: "Start",
  render(_ctx, run) {
    const intro =
      run.mode === "device"
        ? "<p>This sets up Meat Mode on this device.</p>"
        : "<p>Meat Mode keeps your own to-dos and the tasks your agents raise for you in one list, on every device you use.</p>";
    const rest = run.steps.filter((s) => s.part !== null);
    if (!rest.length) return `${intro}<p>There is nothing to set up here yet.</p>`;
    return `${intro}<p>Setup covers:</p><ul class="wiz-list">${rest.map((s) => `<li>${esc(s.title)}</li>`).join("")}</ul>
<p class="muted">Skip any step you like. Each one is also in Settings, and you can run setup again from there.</p>`;
  },
});
