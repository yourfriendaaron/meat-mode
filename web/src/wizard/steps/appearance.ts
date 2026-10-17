import { themeChoiceChanged, themeChoicesHTML } from "../../theme.ts";
import { ORDER, registerStep } from "../registry.ts";

registerStep({
  id: "appearance",
  title: "Appearance",
  part: "device",
  order: ORDER.appearance,
  settings: "s-look",
  render() {
    return themeChoicesHTML("theme-choice");
  },
  change(el, ctx) {
    if (themeChoiceChanged(el, "theme-choice")) ctx.render();
  },
});
