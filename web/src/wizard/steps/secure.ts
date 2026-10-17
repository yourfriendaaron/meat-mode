import type { AppContext } from "../../app-context.ts";
import { chooseTailscaleOnly, claim, claimHTML, claimTyped, messages, tailscaleHTML } from "../../password.ts";
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
  done: (ctx) => !!ctx.setup && (ctx.setup.password !== "none" || ctx.setup.tailscaleOnly),
  render(ctx) {
    const setup = ctx.setup;
    if (!setup) return '<p class="muted">The hub did not say whether it has a password. Set one later in Settings.</p>';
    if (setup.password === "env") {
      return `<p>This hub's password is set in its environment, by <code>MEAT_MODE_PASSWORD_HASH</code> or <code>MEAT_MODE_PASSWORD_HASH_FILE</code>. Change it there.</p>`;
    }
    if (setup.password === "app") {
      return `${messages("wiz") || "<p>This hub has a password.</p>"}<p class="muted">Change it any time in Settings.</p>`;
    }
    const why = setup.tailscaleOnly
      ? "<p>You chose no password: the hub is reached only through Tailscale or another private network. You can still set one here.</p>"
      : "<p>Until the hub has a password, anyone who can reach its address can read and change your list. Set one now.</p>";
    return `${why}${claimHTML("wiz")}${setup.tailscaleOnly ? "" : tailscaleHTML("secure-tailscale")}`;
  },
  async next(ctx: AppContext) {
    const setup = ctx.setup;
    if (!setup || setup.password !== "none") return true;
    const form = claimForm();
    if (claimTyped(form)) return claim("wiz", form!, ctx);
    if (setup.tailscaleOnly) return true;
    ctx.toast("Set a password, choose no password, or skip this step.");
    return false;
  },
  async submit(form, ctx) {
    if (form.dataset.form === "pw-claim") await claim("wiz", form, ctx);
  },
  async act(act, _el, ctx) {
    if (act === "secure-tailscale") await chooseTailscaleOnly(ctx);
  },
});
