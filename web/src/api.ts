// Thin client for the hub's app API (docs/api.md).

import type { Action, Bridge, Device, NewAction, NewBridgeReply, Session, SetupInfo, State } from "./types.ts";

export class ApiError extends Error {
  status: number;
  /** The hub asks for the password again before it does this (docs/api.md, Password again). */
  reauth: boolean;
  /** Seconds from a Retry-After header, or 0 when there was none. */
  retryAfter: number;
  constructor(status: number, message: string, reauth = false, retryAfter = 0) {
    super(message);
    this.status = status;
    this.reauth = reauth;
    this.retryAfter = retryAfter;
  }
}

/** The seconds a Retry-After header asks for, given as seconds or as a date; 0 when absent or unreadable. */
export function retryAfterSeconds(header: string | null, now = Date.now()): number {
  if (!header) return 0;
  const n = Number(header);
  if (Number.isFinite(n)) return Math.max(0, Math.ceil(n));
  const at = Date.parse(header);
  return Number.isNaN(at) ? 0 : Math.max(0, Math.ceil((at - now) / 1000));
}

/** Runs when the hub answers 401 to anything but a login attempt: the session has ended. */
let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

/**
 * Calls one hub route and returns its JSON reply (undefined for 204). Throws
 * ApiError with the hub's message. Features with their own module call it
 * directly rather than adding to `api` below.
 */
export async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    // The hub refuses a state change without this type, even with no body.
    headers: method === "GET" ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
  });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    let reauth = false;
    try {
      const j = await res.json();
      if (j && typeof j.error === "string") msg = j.error;
      reauth = j?.reauth === true;
    } catch {
      // keep the status line
    }
    if (res.status === 401 && path !== "/api/login" && path !== "/api/session") onUnauthorized();
    throw new ApiError(res.status, msg, reauth, retryAfterSeconds(res.headers.get("Retry-After")));
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
  /** Registers a push subscription. An `app` one answers `{device}` and waits for its confirmation; a `web` one answers nothing. */
  pushSubscribe: (sub: PushSubscriptionJSON, kind: "web" | "app" = "web", name = "") =>
    call<{ device: string } | undefined>("POST", "/api/push/subscriptions", { ...sub, kind, ...(name ? { name } : {}) }),
  devices: () => call<Device[]>("GET", "/api/devices"),
  revokeDevice: (id: string) => call<void>("DELETE", `/api/devices/${encodeURIComponent(id)}`),
  bridges: () => call<Bridge[]>("GET", "/api/bridges"),
  /** Adds a machine. The hub may ask for the password again first (ApiError.reauth); send it as password. */
  addBridge: (name: string, password?: string) =>
    call<NewBridgeReply>("POST", "/api/bridges", password === undefined ? { name } : { name, password }),
  revokeBridge: (name: string) => call<void>("DELETE", `/api/bridges/${encodeURIComponent(name)}`),
  setup: () => call<SetupInfo>("GET", "/api/setup"),
  recordSetup: (status: "done" | "dismissed") => call<void>("POST", "/api/setup", { status }),
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
