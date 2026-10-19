// The Appearance section: the theme choice.

import { themeChoiceChanged, themeChoicesHTML } from "../theme.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "look",
  title: "Appearance",
  order: 10,
  render: () => themeChoicesHTML("settings-theme"),
  change(el, ctx) {
    if (themeChoiceChanged(el, "settings-theme")) ctx.render();
  },
});
