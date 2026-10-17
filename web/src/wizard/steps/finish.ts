import { esc } from "../../html.ts";
import { ORDER, registerStep, stepOutcome } from "../registry.ts";

const OUTCOME = { done: "Set up", skipped: "Skipped", open: "Not set up" } as const;

registerStep({
  id: "finish",
  title: "All set",
  part: null,
  order: ORDER.finish,
  skippable: false,
  render(ctx, run) {
    const rest = run.steps.filter((s) => s.part !== null);
    if (!rest.length) return "<p>Setup is complete. You can run it again from Settings.</p>";
    return `<ul class="wiz-summary">${rest
      .map((s) => {
        const outcome = stepOutcome(s, run.progress, ctx);
        const link = s.settings
          ? `<button class="link-btn" data-act="wizard-settings" data-section="${esc(s.settings)}">${esc(s.title)} in Settings</button>`
          : "";
        return `<li><span class="wiz-outcome wiz-${outcome}">${OUTCOME[outcome]}</span><span class="wiz-sum-title">${esc(s.title)}</span>${link}</li>`;
      })
      .join("")}</ul>
<p class="muted">You can change any of these in Settings, or run setup again from there.</p>`;
  },
});
