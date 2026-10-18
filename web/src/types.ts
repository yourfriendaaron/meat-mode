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
  /** Personal items only. */
  checklist?: Check[] | null;
  pending?: boolean;
  seenAt?: string | null;
  firstSeen?: string;
  error?: string;
}

/** One entry of a personal item's checklist. */
export interface Check {
  /** Given by the hub when an edit leaves it empty. */
  id?: string;
  text: string;
  done: boolean;
}

/** What an edit action changes on a personal item. A field left out stays as it is. */
export interface Edit {
  title?: string;
  /** "" clears it. */
  description?: string;
  due?: string;
  /** Removes the due date; never sent with `due`. */
  clearDue?: boolean;
  /** 0 (highest) to 4, as bd. */
  priority?: number;
  /** Replaces the whole list; [] clears it. */
  checklist?: Check[];
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
  /** edit */
  set?: Edit;
  /** edit: the values the fields in `set` held before, filled in by the hub. */
  was?: Edit;
  /** note-edit and note-delete. */
  commentId?: string;
  /** note-edit and note-delete: the comment as it was, filled in by the hub. note-restore: the comment to put back. */
  comment?: Comment;
}

export type ActionKind =
  | "done"
  | "reopen"
  | "snooze"
  | "dismiss"
  | "note"
  | "add"
  | "sync"
  // Personal list only.
  | "edit"
  | "note-edit"
  | "note-delete"
  | "note-restore";

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

/** PUT /api/settings: the name and time zone the hub keeps. A field left out stays as it is. */
export interface SettingsUpdate {
  name?: string;
  timeZone?: string;
}

/** GET /api/setup/status: what the connection check learns from the hub (docs/api.md, Setup). */
export interface SetupStatus {
  /** Where the login password comes from. */
  login: "none" | "app" | "environment";
  /** Whether this request came in under the host of MEAT_MODE_PUBLIC_URL. */
  hostMatches: boolean;
  publicUrl: string;
  pushKeys: boolean;
  ntfy: boolean;
  /** The hub's clock, as an ISO time. */
  time: string;
}
