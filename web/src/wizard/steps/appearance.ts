import { esc } from "../../html.ts";
import { getThemeChoice, setTheme, type ThemeChoice } from "../../theme.ts";
import { ORDER, registerStep } from "../registry.ts";

const CHOICES: [ThemeChoice, string, string][] = [
  ["dark", "Dark", "Light text on a dark background."],
  ["light", "Light", "Dark text on a light background."],
  ["system", "Match this device", "Follows the light or dark setting of this device's system."],
];

registerStep({
  id: "appearance",
  title: "Appearance",
  part: "device",
  order: ORDER.appearance,
  settings: "s-look",
  render() {
    const current = getThemeChoice();
    const options = CHOICES.map(
      ([value, label, hint]) =>
        `<label class="choice" for="theme-${value}"><input type="radio" name="theme-choice" id="theme-${value}" value="${value}"${value === current ? " checked" : ""}><span class="choice-text"><span class="choice-label">${esc(label)}</span><span class="muted small">${esc(hint)}</span></span></label>`,
    ).join("");
    return `<fieldset class="choices"><legend>Choose how Meat Mode looks on this device.</legend>${options}</fieldset>`;
  },
  change(el, ctx) {
    if (!(el instanceof HTMLInputElement) || el.name !== "theme-choice") return;
    setTheme(el.value as ThemeChoice);
    ctx.render();
  },
});
