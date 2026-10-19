// Copying a command or a link the app shows, for its Copy buttons.

/**
 * Copies the text of the element with that id. Where the clipboard is out of
 * reach, such as over plain HTTP, it selects the text and asks the browser to
 * copy the selection, which leaves it selected for copying by hand.
 */
export async function copyText(id: string): Promise<void> {
  const el = document.getElementById(id);
  if (!el) return;
  try {
    await navigator.clipboard.writeText(el.textContent ?? "");
  } catch {
    const range = document.createRange();
    range.selectNodeContents(el);
    const sel = getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
    document.execCommand("copy");
  }
}
