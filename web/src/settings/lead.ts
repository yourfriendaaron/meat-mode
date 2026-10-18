import { hubLead, leadError, leadSelect, loadLead, saveLead } from "../lead.ts";
import { esc } from "../html.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "lead",
  title: "Early reminder",
  order: 22,
  render(ctx) {
    const lead = hubLead();
    if (lead === null) {
      const error = leadError();
      if (!error) loadLead(ctx);
      return error
        ? `<p class="row-error">Could not load the setting: ${esc(error)}</p><button class="btn" data-act="lead-retry">Try again</button>`
        : '<p class="muted">Loading</p>';
    }
    return `<div class="set-field"><label for="set-lead">Remind me early</label>${leadSelect("set-lead", lead, false, ' aria-describedby="set-lead-hint"')}
<p class="muted small" id="set-lead-hint">A reminder always comes at the due time. Pick a time to get one more before it, on every list. A personal to-do can set its own when you edit it.</p></div>`;
  },
  act(act, _el, ctx) {
    if (act === "lead-retry") loadLead(ctx, true);
  },
  change(el, ctx) {
    if (el.id !== "set-lead") return;
    void saveLead(Number((el as HTMLSelectElement).value), ctx).then((ok) => ok && ctx.toast("Saved"));
  },
});
