// Thin client for the hub's app API (docs/api.md).

import type { Action, Attachment, Bridge, Device, NewAction, NewBridgeReply, Occurrences, Session, SetupInfo, State } from "./types.ts";

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

/** "a minute", or the seconds or minutes a Retry-After asked for. */
export function waitText(seconds: number): string {
  if (seconds <= 0 || seconds === 60) return "a minute";
  if (seconds < 60) return seconds === 1 ? "1 second" : `${seconds} seconds`;
  const m = Math.ceil(seconds / 60);
  return m === 1 ? "a minute" : `${m} minutes`;
}

/** What one place says for the failures the hub answers in its own way. Each has a default. */
export interface ErrorPhrases {
  /** A 401, such as "Wrong password." Default: the hub's message. */
  unauthorized?: string;
  /** A 403. Default: the hub's message. */
  forbidden?: string;
  /** A 403 that asks for the password again (ApiError.reauth). Default: as forbidden. */
  reauth?: string;
  /** What a 429 says before "Wait … and try again.", which names the hub's Retry-After. Default "Too many tries". */
  tooMany?: string;
  /** Wraps any other failure's message, such as "Could not log in: …". Default: the message as it is. */
  other?: (message: string) => string;
}

/** The words for a failed request or any other thrown error, the same way everywhere. */
export function errorText(e: unknown, phrases: ErrorPhrases = {}): string {
  const other = phrases.other ?? ((m: string) => m);
  if (!(e instanceof ApiError)) return other(e instanceof Error ? e.message : String(e));
  if (e.status === 429) return `${phrases.tooMany ?? "Too many tries"}. Wait ${waitText(e.retryAfter)} and try again.`;
  if (e.status === 401 && phrases.unauthorized !== undefined) return phrases.unauthorized;
  if (e.status === 403 && e.reauth && phrases.reauth !== undefined) return phrases.reauth;
  if (e.status === 403 && phrases.forbidden !== undefined) return phrases.forbidden;
  return other(e.message);
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
  return reply<T>(res, path);
}

/**
 * Sends a file's bytes as they are, for the two routes that take one
 * (docs/api.md, App API): an attachment and a restore. `timeout` is in ms,
 * 0 for none; the hub allows 30 minutes to read either body.
 */
export async function sendFile<T>(path: string, file: Blob, timeout = 30 * 60 * 1000): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/octet-stream" },
    body: file,
    signal: timeout ? AbortSignal.timeout(timeout) : undefined,
  });
  return reply<T>(res, path);
}

/** A hub reply's JSON (undefined for 204), or ApiError with the hub's message. */
async function reply<T>(res: Response, path: string): Promise<T> {
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

/** Where the hub serves an attachment's bytes; an <img> or a link loads it as the page's own origin. */
export function attachmentURL(id: string): string {
  return `/api/attachments/${encodeURIComponent(id)}`;
}

export const api = {
  session: () => call<Session>("GET", "/api/session"),
  login: (password: string) => call<void>("POST", "/api/login", { password }),
  logout: () => call<void>("POST", "/api/logout"),
  state: () => call<State>("GET", "/api/state"),
  postAction: (a: NewAction) => call<Action>("POST", "/api/actions", a),
  /** Throws ApiError 409 when the action was already applied; actions.ts then posts its inverse. */
  cancelAction: (id: string) => call<Action>("POST", `/api/actions/${encodeURIComponent(id)}/cancel`),
  actions: (limit = 30) => call<Action[]>("GET", `/api/actions?limit=${limit}`),
  /** The actions on one item, newest first, its sub-tasks' adds included (docs/api.md, Item history). */
  history: (key: string, limit = 500) => call<Action[]>("GET", `/api/items/${encodeURIComponent(key)}/history?limit=${limit}`),
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
  /** Attaches a file to an item of the hub's own lists (docs/api.md, Attachments). The hub sniffs its type. */
  attach: (key: string, file: File) =>
    sendFile<Attachment>(`/api/items/${encodeURIComponent(key)}/attachments?name=${encodeURIComponent(file.name)}`, file),
  /** Removes an attachment; restoreAttachment puts it back until the hub purges it. */
  removeAttachment: (id: string) => call<void>("DELETE", attachmentURL(id)),
  /** Throws ApiError 404 once the attachment is purged, 409 when putting it back would pass a cap. */
  restoreAttachment: (id: string) => call<Attachment>("POST", `${attachmentURL(id)}/restore`),
  /** The later dates of repeating items in [from, to), at most 62 days (docs/api.md, Month view). */
  occurrences: (from: Date, to: Date) =>
    call<Occurrences>("GET", `/api/occurrences?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`),
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
