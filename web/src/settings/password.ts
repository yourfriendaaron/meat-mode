import { change, changeHTML, claim, claimHTML } from "../password.ts";
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
    // The hub serves only its claim step until it has a password, so this shows only if the password went away meanwhile.
    return `<p class="muted">This hub has no password. It answers nothing else until it has one.</p>${claimHTML("set")}`;
  },
  async submit(form, ctx) {
    if (form.dataset.form === "pw-claim") await claim("set", form, ctx);
    else if (form.dataset.form === "pw-change") await change("set", form, ctx);
  },
});
