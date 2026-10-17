// Wire types, mirroring internal/api/types.go. Times are RFC 3339 strings.

export interface Comment {
  id: string;
  author: string;
  text: string;
  createdAt: string;
}

export interface Item {
  key: string;
  bridge: string;
  repo: string;
  id: string;
  title: string;
  description?: string;
  status: string; // open, in_progress, blocked, deferred, closed
  closeReason?: string;
  priority: number;
  labels?: string[];
  createdBy?: string;
  dueAt?: string | null;
  deferUntil?: string | null;
  createdAt: string;
  updatedAt: string;
  closedAt?: string | null;
  comments: Comment[] | null;
  pending?: boolean;
  seenAt?: string | null;
  firstSeen?: string;
  error?: string;
}

export interface Repo {
  bridge: string;
  name: string;
  personal: boolean;
  optedIn: boolean;
  /** A sync switch the machine has not applied yet; optedIn shows where it was switched to. */
  pending?: boolean;
}

export interface Bridge {
  name: string;
  createdAt: string;
  lastSeen?: string | null;
  version?: string;
  bdVersion?: string;
}

export interface Payload {
  until?: string | null;
  previous?: string | null;
  text?: string;
  title?: string;
  due?: string | null;
  on?: boolean;
}

export type ActionKind = "done" | "reopen" | "snooze" | "dismiss" | "note" | "add" | "sync";

export interface NewAction {
  id: string;
  kind: ActionKind;
  key?: string;
  bridge?: string;
  repo?: string;
  payload: Payload;
}

export interface Action {
  id: string;
  seq: number;
  kind: ActionKind;
  bridge: string;
  repo: string;
  beadId?: string;
  payload: Payload;
  author: string;
  status: "pending" | "applied" | "failed" | "cancelled";
  error?: string;
  createdAt: string;
  appliedAt?: string | null;
}

export interface State {
  items: Item[] | null;
  repos: Repo[] | null;
  bridges: Bridge[] | null;
  unapplied: number;
  user: string;
  serverTime: string;
}

export interface NewBridgeReply {
  name: string;
  token: string;
}

/** GET /api/session: whether the hub has a login, and whether this browser is logged in. */
export interface Session {
  login: boolean;
  authenticated: boolean;
}

/** GET /api/setup: whether to offer the setup wizard, and what is already set (docs/api.md, Setup). */
export interface SetupInfo {
  /** "new": no items, no machines, no record. "in-use": items or machines, no record. */
  status: "new" | "in-use" | "done" | "dismissed";
  user: string;
  /** IANA time zone kept in the app, "" until chosen. */
  timeZone: string;
  /** Where the login password comes from. */
  password: "env" | "app" | "none";
  tailscaleOnly: boolean;
}
