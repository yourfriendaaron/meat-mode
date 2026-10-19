// Descriptions and notes as Markdown (docs/api.md, Descriptions and notes in
// Markdown). The text may come from an agent, so nothing in it reaches the
// page as markup of its own.

import { esc } from "./html.ts";

/** Renders text as Markdown to safe HTML for the detail view. */
export function renderMarkdown(text: string): string {
  return `<p>${esc(text)}</p>`;
}
