import { startWizard } from "../wizard/wizard.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "setup",
  title: "Setup",
  order: 50,
  render() {
    return `<p class="muted">Go through setup again, or only the steps for this device.</p>
<div class="btn-row"><button class="btn" data-act="setup-again">Run setup again</button><button class="btn" data-act="setup-device">Set up this device</button></div>`;
  },
  act(act, _el, ctx) {
    if (act === "setup-again") startWizard("all", ctx);
    else if (act === "setup-device") startWizard("device", ctx);
  },
});
