// Loaded in <head> before first paint so a stored light theme does not flash dark.
import { applyTheme, getTheme } from "./theme.ts";

applyTheme(getTheme());
