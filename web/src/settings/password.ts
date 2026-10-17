import { change, changeHTML, chooseTailscaleOnly, claim, claimHTML, tailscaleHTML } from "../password.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "password",
  title: "Password",
  order: 35,
  applies: (ctx) => !!ctx.setup,
  render(ctx) {
    const setup = ctx.setup!;
    if (setup.password === "env") {
      return `<p class="muted">The password is set in the hub's environment, by <code>MEAT_MODE_PASSWORD_HASH</code> or <code>MEAT_MODE_PASSWORD_HASH_FILE</code>. Change it there.</p>`;
    }
    if (setup.password === "app") return changeHTML("set");
    const why = setup.tailscaleOnly
      ? '<p class="muted">This hub has no password: you keep it on Tailscale or another private network.</p>'
      : '<p class="muted">This hub has no password. Anyone who can reach its address can read and change your list.</p>';
    return `${why}${claimHTML("set")}${setup.tailscaleOnly ? "" : tailscaleHTML("password-tailscale")}`;
  },
  async submit(form, ctx) {
    if (form.dataset.form === "pw-claim") await claim("set", form, ctx);
    else if (form.dataset.form === "pw-change") await change("set", form, ctx);
  },
  async act(act, _el, ctx) {
    if (act === "password-tailscale") await chooseTailscaleOnly(ctx);
  },
});
