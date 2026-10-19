import type { AppContext } from "../../app-context.ts";
import { claim, claimHTML, claimTyped, messages } from "../../password.ts";
import { ORDER, registerStep } from "../registry.ts";

function claimForm(): HTMLFormElement | null {
  return document.querySelector<HTMLFormElement>('[data-step="secure"] form[data-form="pw-claim"]');
}

registerStep({
  id: "secure",
  title: "Secure the hub",
  part: "hub",
  order: ORDER.secure,
  settings: "s-password",
  done: (ctx) => !!ctx.setup && ctx.setup.password !== "none",
  render(ctx) {
    const setup = ctx.setup;
    if (!setup) return '<p class="muted">The hub did not say whether it has a password. Check it later in Settings.</p>';
    if (setup.password === "env") {
      return `<p>This hub's password is set in its environment, by <code>MEAT_MODE_PASSWORD_HASH</code> or <code>MEAT_MODE_PASSWORD_HASH_FILE</code>. Change it there.</p>`;
    }
    if (setup.password === "app") {
      return `${messages("wiz") || "<p>This hub has a password.</p>"}<p class="muted">Change it any time in Settings.</p>`;
    }
    // The hub serves only its claim step until it has a password, so this shows only if the password went away meanwhile.
    return `<p>This hub has no password. It answers nothing else until it has one.</p>${claimHTML("wiz")}`;
  },
  async next(ctx: AppContext) {
    const setup = ctx.setup;
    if (!setup || setup.password !== "none") return true;
    const form = claimForm();
    if (claimTyped(form)) return claim("wiz", form!, ctx);
    ctx.toast("Set a password, or skip this step.");
    return false;
  },
  async submit(form, ctx) {
    if (form.dataset.form === "pw-claim") await claim("wiz", form, ctx);
  },
});
