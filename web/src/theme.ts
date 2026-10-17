// Theme choice: Dracula (dark) by default, Alucard (light) behind the toggle,
// or whichever of the two this device's system setting asks for.

import { esc } from "./html.ts";

export type Theme = "dark" | "light";
/** What the user picked; "system" follows the device's light or dark setting. */
export type ThemeChoice = Theme | "system";

const KEY = "meat-mode.theme";
const BG: Record<Theme, string> = { dark: "#282A36", light: "#FFFBEB" };
const SYSTEM_LIGHT = "(prefers-color-scheme: light)";

export function getThemeChoice(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY);
    return v === "light" || v === "system" ? v : "dark";
  } catch {
    return document.documentElement.dataset.theme === "light" ? "light" : "dark";
  }
}

/** The theme on screen: the choice, with "system" resolved for this device. */
export function getTheme(): Theme {
  const choice = getThemeChoice();
  if (choice !== "system") return choice;
  return matchMedia(SYSTEM_LIGHT).matches ? "light" : "dark";
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", BG[theme]);
}

export function setTheme(choice: ThemeChoice): void {
  try {
    localStorage.setItem(KEY, choice);
  } catch {
    // storage blocked: the choice lasts for this page only
  }
  applyTheme(choice === "system" ? (matchMedia(SYSTEM_LIGHT).matches ? "light" : "dark") : choice);
}

/** Follows the device's setting while the choice is "system"; onChange runs after each switch. */
export function followSystemTheme(onChange?: () => void): void {
  matchMedia(SYSTEM_LIGHT).addEventListener("change", () => {
    if (getThemeChoice() !== "system") return;
    applyTheme(getTheme());
    onChange?.();
  });
}

const CHOICES: [ThemeChoice, string, string][] = [
  ["dark", "Dark", "Light text on a dark background."],
  ["light", "Light", "Dark text on a light background."],
  ["system", "Match this device", "Follows the light or dark setting of this device's system."],
];

/** The light, dark and device radio group; name keeps the ids unique where more than one is on a page. */
export function themeChoicesHTML(name: string): string {
  const current = getThemeChoice();
  const options = CHOICES.map(
    ([value, label, hint]) =>
      `<label class="choice" for="${name}-${value}"><input type="radio" name="${name}" id="${name}-${value}" value="${value}"${value === current ? " checked" : ""}><span class="choice-text"><span class="choice-label">${esc(label)}</span><span class="muted small">${esc(hint)}</span></span></label>`,
  ).join("");
  return `<fieldset class="choices"><legend>Choose how Meat Mode looks on this device.</legend>${options}</fieldset>`;
}

/** Applies a change event from a themeChoicesHTML(name) group; false when the event is from something else. */
export function themeChoiceChanged(el: HTMLElement, name: string): boolean {
  if (!(el instanceof HTMLInputElement) || el.name !== name) return false;
  setTheme(el.value as ThemeChoice);
  return true;
}
