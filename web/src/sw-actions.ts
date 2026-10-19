// The actions a notification button posts without opening the app
// (docs/api.md, Agent push). Kept apart from sw.ts so tests can import it.

export type ButtonAction = "retry" | "done" | "snooze";

/** How long the Snooze button snoozes for. */
export const SNOOZE_MS = 60 * 60 * 1000;

/** Whether `action` is a button that posts to the hub instead of opening the app. */
export function isPostedAction(action: string): action is ButtonAction {
  return action === "retry" || action === "done" || action === "snooze";
}

/** The body of the POST /api/actions a button sends. Retry is an `ask` on the item. */
export function actionBody(action: ButtonAction, key: string, id: string, now: Date): object {
  if (action === "retry") return { id, kind: "ask", key, payload: {} };
  if (action === "done") return { id, kind: "done", key, payload: {} };
  return { id, kind: "snooze", key, payload: { until: new Date(now.getTime() + SNOOZE_MS).toISOString(), previous: null } };
}

/** The words for a refused or failed button action: the hub's own reason, capitalised, else a plain fallback. */
export function failureText(status: number, body: string): string {
  let reason = "";
  try {
    const j = JSON.parse(body);
    if (j && typeof j.error === "string") reason = j.error.trim();
  } catch {
    // Not JSON: use the fallback.
  }
  if (!reason) return status ? "The hub could not do that." : "Could not reach the hub.";
  reason = reason[0].toUpperCase() + reason.slice(1);
  return /[.!?]$/.test(reason) ? reason : `${reason}.`;
}
