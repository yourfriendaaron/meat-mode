import { profileForm, saveProfile } from "../../profile.ts";
import { ORDER, registerStep } from "../registry.ts";

registerStep({
  id: "profile",
  title: "Name and time zone",
  part: "hub",
  order: ORDER.profile,
  settings: "s-profile",
  done: (ctx) => !!ctx.setup?.timeZone,
  render(ctx) {
    return `<p>Confirm your name and time zone. Continue keeps them.</p>${profileForm(ctx, "wiz-profile", false)}`;
  },
  async next(ctx) {
    const form = document.querySelector<HTMLFormElement>("#wizard form[data-form=profile]");
    return form ? saveProfile(form, ctx) : true;
  },
  async submit(form, ctx) {
    if (await saveProfile(form, ctx)) ctx.toast("Saved");
  },
});
