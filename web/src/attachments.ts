// Files attached to an item of the hub's own lists (docs/api.md,
// Attachments): the detail view's section, and its events through ext.ts.

import type { Handlers } from "./app-context.ts";
import type { Item } from "./types.ts";

/** The detail view's Files section, or "" for an item that takes none. */
export function attachmentsHTML(_item: Item, _editable: boolean): string {
  return "";
}

/** Events inside [data-attachments]. */
export const attachmentHandlers: Handlers = {};
