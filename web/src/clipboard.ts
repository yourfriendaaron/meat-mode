// Copying a command, a link or an id the app shows, for its Copy buttons.

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
    selectAndCopy(el);
  }
}

/**
 * Copies a string that is not on screen as it is, such as an id. Where the
 * clipboard is out of reach it copies from a field it adds and removes again.
 */
export async function copyValue(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const field = document.createElement("textarea");
    field.value = text;
    field.setAttribute("readonly", "");
    field.className = "sr";
    document.body.append(field);
    field.select();
    // iOS Safari ignores the selection a readonly field's select() makes;
    // naming the range keeps the copy below working there.
    field.setSelectionRange(0, text.length);
    document.execCommand("copy");
    field.remove();
  }
}

function selectAndCopy(el: Element) {
  const range = document.createRange();
  range.selectNodeContents(el);
  const sel = getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
  document.execCommand("copy");
}
