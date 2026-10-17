// Theme choice: Dracula (dark) by default, Alucard (light) behind the toggle,
// or whichever of the two this device's system setting asks for.

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
