// Theme choice: Dracula (dark) by default, Alucard (light) behind the toggle.

export type Theme = "dark" | "light";

const KEY = "meat-mode.theme";
const BG: Record<Theme, string> = { dark: "#282A36", light: "#FFFBEB" };

export function getTheme(): Theme {
  try {
    return localStorage.getItem(KEY) === "light" ? "light" : "dark";
  } catch {
    return document.documentElement.dataset.theme === "light" ? "light" : "dark";
  }
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", BG[theme]);
}

export function setTheme(theme: Theme): void {
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // storage blocked: the choice lasts for this page only
  }
  applyTheme(theme);
}
