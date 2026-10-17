// Thin client for the hub's app API (docs/api.md).

import type { Action, Bridge, NewAction, NewBridgeReply, Session, State } from "./types.ts";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** Runs when the hub answers 401 to anything but a login attempt: the session has ended. */
let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    // The hub refuses a state change without this type, even with no body.
    headers: method === "GET" ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
  });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      const j = await res.json();
      if (j && typeof j.error === "string") msg = j.error;
    } catch {
      // keep the status line
    }
    if (res.status === 401 && path !== "/api/login") onUnauthorized();
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const api = {
  session: () => call<Session>("GET", "/api/session"),
  login: (password: string) => call<void>("POST", "/api/login", { password }),
  logout: () => call<void>("POST", "/api/logout"),
  state: () => call<State>("GET", "/api/state"),
  postAction: (a: NewAction) => call<Action>("POST", "/api/actions", a),
  /** Resolves to true when cancelled, false when the action was already applied (409). */
  cancelAction: async (id: string): Promise<boolean> => {
    try {
      await call<Action>("POST", `/api/actions/${encodeURIComponent(id)}/cancel`);
      return true;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) return false;
      throw e;
    }
  },
  actions: (limit = 30) => call<Action[]>("GET", `/api/actions?limit=${limit}`),
  seen: (key: string) => call<void>("POST", `/api/items/${encodeURIComponent(key)}/seen`),
  pushKey: () => call<{ publicKey: string }>("GET", "/api/push/key"),
  pushSubscribe: (sub: PushSubscriptionJSON) => call<void>("POST", "/api/push/subscriptions", sub),
  bridges: () => call<Bridge[]>("GET", "/api/bridges"),
  addBridge: (name: string) => call<NewBridgeReply>("POST", "/api/bridges", { name }),
  revokeBridge: (name: string) => call<void>("DELETE", `/api/bridges/${encodeURIComponent(name)}`),
};

/** A fresh action id. randomUUID needs a secure context, so plain-HTTP LAN use falls back to getRandomValues. */
export function newId(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}
