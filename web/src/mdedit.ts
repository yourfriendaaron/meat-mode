// Markdown typing in the note and description boxes, as GitHub's comment box
// does it, with no toolbar: a list continues on Enter, Tab nests a list item,
// Ctrl or Cmd with B, I, E or K wraps the selection, and a URL pasted over
// selected text makes a link. The box stays a plain textarea and the text
// stays as typed; markdown.ts renders it.

/** A replacement of value[start, end) by text, then the selection to set. */
export interface Edit {
  start: number;
  end: number;
  text: string;
  selStart: number;
  selEnd: number;
}

interface Box {
  value: string;
  start: number;
  end: number;
}

/** Indent, list marker (bullet, number or task box), and the text after it. */
const ITEM = /^(\s*)([-*+]|\d{1,9}[.)])(\s+)(\[[ xX]\]\s+)?(.*)$/;
const INDENT = "  ";

function lineAt(value: string, pos: number): { start: number; end: number; text: string } {
  const start = value.lastIndexOf("\n", pos - 1) + 1;
  const nl = value.indexOf("\n", pos);
  const end = nl < 0 ? value.length : nl;
  return { start, end, text: value.slice(start, end) };
}

/** Whether pos sits inside a fenced code block, where list rules do not apply. */
function inFence(value: string, pos: number): boolean {
  let open = false;
  for (const line of value.slice(0, pos).split("\n").slice(0, -1)) if (/^\s*(```|~~~)/.test(line)) open = !open;
  return open;
}

/**
 * Enter on a list item. With text after the marker, the next line starts with
 * the same marker (the next number, an empty task box). On an item with no
 * text, the marker goes and the list ends. Null leaves Enter to the browser.
 */
export function enterEdit(b: Box): Edit | null {
  if (b.start !== b.end || inFence(b.value, b.start)) return null;
  const line = lineAt(b.value, b.start);
  const m = ITEM.exec(line.text);
  if (!m) return null;
  const [, indent, marker, gap, task, rest] = m;
  const prefixLen = line.text.length - rest.length;
  if (b.start - line.start < prefixLen) return null;
  if (!rest.trim()) {
    return { start: line.start, end: line.end, text: "", selStart: line.start, selEnd: line.start };
  }
  const num = /^(\d+)([.)])$/.exec(marker);
  const next = num ? `${Number(num[1]) + 1}${num[2]}` : marker;
  const text = `\n${indent}${next}${gap}${task ? "[ ] " : ""}`;
  const at = b.start + text.length;
  return { start: b.start, end: b.end, text, selStart: at, selEnd: at };
}

/** Tab or Shift+Tab with the selection on list items: nests or un-nests them. */
export function indentEdit(b: Box, out: boolean): Edit | null {
  if (inFence(b.value, b.start)) return null;
  const first = lineAt(b.value, b.start);
  const last = lineAt(b.value, b.end > b.start && b.value[b.end - 1] === "\n" ? b.end - 1 : b.end);
  const lines = b.value.slice(first.start, last.end).split("\n");
  if (!lines.every((l) => ITEM.test(l))) return null;
  let delta0 = 0;
  let total = 0;
  const changed = lines.map((l, i) => {
    let d: number;
    let s: string;
    if (out) {
      const cut = l.startsWith(INDENT) ? INDENT.length : l.startsWith("\t") || l.startsWith(" ") ? 1 : 0;
      s = l.slice(cut);
      d = -cut;
    } else {
      s = INDENT + l;
      d = INDENT.length;
    }
    if (i === 0) delta0 = d;
    total += d;
    return s;
  });
  if (total === 0) return null;
  const text = changed.join("\n");
  return {
    start: first.start,
    end: last.end,
    text,
    selStart: Math.max(first.start, b.start + delta0),
    selEnd: Math.max(first.start, b.end + total),
  };
}

const WRAP: Record<string, string> = { b: "**", i: "_", e: "`" };

/**
 * Ctrl or Cmd with B (bold), I (italic), E (code) or K (link). B, I and E
 * wrap the selection, or unwrap it when it is already wrapped; with nothing
 * selected the cursor lands between the marks. K makes [text](url) with the
 * url selected to type over.
 */
export function shortcutEdit(b: Box, key: string): Edit | null {
  const k = key.toLowerCase();
  const sel = b.value.slice(b.start, b.end);
  if (k === "k") {
    const isUrl = /^https?:\/\/\S+$/i.test(sel);
    const text = isUrl ? `[](${sel})` : `[${sel}](url)`;
    const at = isUrl ? b.start + 1 : b.start + sel.length + 3;
    return { start: b.start, end: b.end, text, selStart: at, selEnd: isUrl ? at : at + 3 };
  }
  const mark = WRAP[k];
  if (!mark) return null;
  const n = mark.length;
  if (b.start >= n && b.value.slice(b.start - n, b.start) === mark && b.value.slice(b.end, b.end + n) === mark) {
    return { start: b.start - n, end: b.end + n, text: sel, selStart: b.start - n, selEnd: b.end - n };
  }
  return { start: b.start, end: b.end, text: mark + sel + mark, selStart: b.start + n, selEnd: b.end + n };
}

/** A web address pasted over selected text links the text to it. */
export function pasteEdit(b: Box, pasted: string): Edit | null {
  const url = pasted.trim();
  if (b.start === b.end || !/^https?:\/\/\S+$/i.test(url)) return null;
  const sel = b.value.slice(b.start, b.end);
  if (sel.includes("\n") || /^https?:\/\//i.test(sel.trim())) return null;
  const text = `[${sel}](${url})`;
  const at = b.start + text.length;
  return { start: b.start, end: b.end, text, selStart: at, selEnd: at };
}

/**
 * Applies an edit through the browser's own text insertion where it can, so
 * Undo takes it back in one step, and fires input as typing would.
 */
function apply(ta: HTMLTextAreaElement, e: Edit) {
  ta.focus();
  ta.setSelectionRange(e.start, e.end);
  let done = false;
  try {
    done = e.text === "" && e.start !== e.end ? document.execCommand("delete") : document.execCommand("insertText", false, e.text);
  } catch {
    done = false;
  }
  if (!done || ta.value.slice(e.start, e.start + e.text.length) !== e.text) {
    ta.setRangeText(e.text, e.start, e.end, "end");
    ta.dispatchEvent(new Event("input", { bubbles: true }));
  }
  ta.setSelectionRange(e.selStart, e.selEnd);
}

function box(ta: HTMLTextAreaElement): Box {
  return { value: ta.value, start: ta.selectionStart, end: ta.selectionEnd };
}

function target(e: Event): HTMLTextAreaElement | null {
  const t = e.target;
  return t instanceof HTMLTextAreaElement && t.classList.contains("md-input") ? t : null;
}

/** Listens on the document for keys and pastes in every textarea.md-input. */
export function attachMarkdownTyping(doc: Document = document) {
  doc.addEventListener(
    "keydown",
    (e) => {
      const ta = target(e);
      if (!ta || e.isComposing || e.defaultPrevented) return;
      let edit: Edit | null = null;
      const mod = (e.ctrlKey || e.metaKey) && !e.altKey;
      if (e.key === "Enter" && !mod && !e.shiftKey && !e.altKey) edit = enterEdit(box(ta));
      else if (e.key === "Tab" && !mod && !e.altKey) edit = indentEdit(box(ta), e.shiftKey);
      else if (mod && !e.shiftKey && e.key.length === 1) edit = shortcutEdit(box(ta), e.key);
      if (!edit) return;
      e.preventDefault();
      e.stopPropagation();
      apply(ta, edit);
    },
    true,
  );
  doc.addEventListener("paste", (e) => {
    const ta = target(e);
    const text = e.clipboardData?.getData("text/plain");
    if (!ta || !text) return;
    const edit = pasteEdit(box(ta), text);
    if (!edit) return;
    e.preventDefault();
    apply(ta, edit);
  });
}
