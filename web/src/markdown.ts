// Descriptions and notes as Markdown (docs/api.md, Descriptions and notes in
// Markdown). The text may come from an agent, so nothing in it reaches the
// page as markup of its own.

import MarkdownIt from "markdown-it";
import type { StateCore, Token } from "markdown-it";

const md = new MarkdownIt("default", { html: false, linkify: false, typographer: false, breaks: true });

/** The only addresses a link may have. Anything else shows as the link's text. */
const SAFE_HREF = /^(https?:|mailto:)/i;

export function safeHref(href: string): boolean {
  return SAFE_HREF.test(href.trim());
}

// Every address reaches the token stream, so the one check below decides
// them all alike: inline links, references and autolinks.
md.validateLink = () => true;

/** The plain text of inline tokens, for an image's alt text. */
function plainText(tokens: Token[] | null): string {
  let s = "";
  for (const t of tokens ?? []) {
    if (t.type === "text" || t.type === "code_inline") s += t.content;
    else if (t.type === "softbreak" || t.type === "hardbreak") s += " ";
    else if (t.children) s += plainText(t.children);
  }
  return s;
}

/**
 * Drops each link whose address is not http, https or mailto, keeping its
 * text, and turns each image into a link named by its alt text: the page
 * loads no image from elsewhere, and a remote one would tell its host when
 * the item was opened. An image inside a link that was kept adds its text
 * alone: a link cannot hold a link.
 */
function guardLinks(state: StateCore) {
  for (const block of state.tokens) {
    if (block.type !== "inline" || !block.children) continue;
    const out: Token[] = [];
    const dropped: boolean[] = [];
    for (const t of block.children) {
      if (t.type === "link_open") {
        const ok = safeHref(String(t.attrGet("href") ?? ""));
        dropped.push(!ok);
        if (ok) out.push(t);
      } else if (t.type === "link_close") {
        if (!dropped.pop()) out.push(t);
      } else if (t.type === "image") {
        const src = String(t.attrGet("src") ?? "");
        const text = new state.Token("text", "", 0);
        text.content = plainText(t.children) || src;
        if (!safeHref(src) || dropped.at(-1) === false) {
          out.push(text);
          continue;
        }
        const open = new state.Token("link_open", "a", 1);
        open.attrs = [["href", src]];
        const title = t.attrGet("title");
        if (title) open.attrs.push(["title", String(title)]);
        out.push(open, text, new state.Token("link_close", "a", -1));
      } else {
        out.push(t);
      }
    }
    block.children = out;
  }
}
md.core.ruler.push("guard_links", guardLinks);

const rules = md.renderer.rules;

rules.link_open = (tokens, idx, options, _env, self) => {
  const t = tokens[idx];
  t.attrSet("target", "_blank");
  t.attrSet("rel", "noopener noreferrer");
  return self.renderToken(tokens, idx, options);
};

// A table column's alignment is a class, not an inline style.
for (const rule of ["th_open", "td_open"]) {
  rules[rule] = (tokens, idx, options, _env, self) => {
    const t = tokens[idx];
    const align = /text-align:(left|right|center)/.exec(String(t.attrGet("style") ?? ""))?.[1];
    t.attrs = align ? [["class", `md-${align}`]] : null;
    return self.renderToken(tokens, idx, options);
  };
}

// A heading is one size, smaller than the detail's own, and takes no place
// in the page's outline or an id another element could clash with.
rules.heading_open = () => '<p class="md-h">';
rules.heading_close = () => "</p>\n";

// A fence's info string names no class: the code shows as it is.
rules.fence = (tokens, idx) => `<pre><code>${md.utils.escapeHtml(tokens[idx].content)}</code></pre>\n`;

// A line break without the newline after it: paragraphs keep white-space
// pre-wrap, so a plain-text description looks as it did before Markdown.
rules.softbreak = () => "<br>";
rules.hardbreak = () => "<br>";

/** Renders text as Markdown to safe HTML for the detail view. */
export function renderMarkdown(text: string): string {
  return md.render(text);
}
