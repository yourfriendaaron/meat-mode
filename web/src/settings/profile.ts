import { profileForm, saveProfile } from "../profile.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "profile",
  title: "Name and time zone",
  order: 15,
  render: (ctx) => profileForm(ctx, "set-profile", true),
  async submit(form, ctx) {
    if (await saveProfile(form, ctx)) ctx.toast("Saved");
  },
});
