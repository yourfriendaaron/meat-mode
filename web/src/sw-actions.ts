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
