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
  /** Personal items only. */
  repeat?: Repeat | null;
  /** Minutes before the due time an early reminder comes; unset uses the hub's reminderLead. Personal items only. */
  remindBefore?: number | null;
  /** The latest agent run on this item; absent when no agent was ever asked. */
  run?: Run | null;
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
  repeat?: Repeat;
  /** Ends the repeat; never sent with `repeat`. */
  clearRepeat?: boolean;
  /** Minutes, 0 (no early reminder) to MAX_LEAD_MINUTES. */
  remindBefore?: number;
  /** Goes back to the hub's reminderLead; never sent with `remindBefore`. */
  clearRemindBefore?: boolean;
}

export type RepeatUnit = "day" | "week" | "month" | "year";

/** Caps Repeat.interval. */
export const MAX_REPEAT_INTERVAL = 100;
/** Caps a reminder lead time: one week. */
export const MAX_LEAD_MINUTES = 7 * 24 * 60;

/** Makes a personal item come back after it is done (docs/api.md, Repeating to-dos). The item's due time is the current occurrence. */
export interface Repeat {
  every: RepeatUnit;
  /** Every n units, 1 to MAX_REPEAT_INTERVAL; left out means 1. */
  interval?: number;
  /** A weekly rule's days, 0 (Sunday) to 6. Left out, the due date's weekday. */
  weekdays?: number[];
  /** Counts the next date from the day the item is done. */
  fromDone?: boolean;
  /** IANA zone whose wall-clock time the rule keeps. */
  timeZone: string;
  /** Last date the series may land on, "YYYY-MM-DD" in timeZone, inclusive. Left out, it repeats for good. */
  until?: string;
  /** Set by the hub; a client sends none. */
  start?: string;
}

export interface Repo {
  bridge: string;
  name: string;
  personal: boolean;
  optedIn: boolean;
  /** An ask may wake an agent on this repo's items. Off by default. */
  agentsAllowed?: boolean;
  /** A sync switch the machine has not applied yet; optedIn shows where it was switched to. */
  pending?: boolean;
  /** An agents switch the machine has not applied yet; agentsAllowed shows where it was switched to. */
  agentsPending?: boolean;
}

/** An agent run's state (docs/api.md, Agent runs). No run on an item means no agent was ever asked. */
export type RunState = "working" | "waiting" | "done" | "stopped";

/** Why a run stopped. */
export type RunReason =
  | "stopped"
  | "usage-limit"
  | "timeout"
  | "budget"
  | "run-limit"
  | "busy"
  | "not-allowed"
  | "no-reply"
  | "failed"
  | "crashed"
  | "restarted"
  | "offline";

/** One agent run on one item, as its machine reports it. */
export interface Run {
  /** A new run gets a new id. */
  id: string;
  state: RunState;
  /** The agent's name to show, such as "planner". */
  agent: string;
  /** One line of what the agent is doing now; while settled, the last thing it did. */
  activity?: string;
  startedAt: string;
  endedAt?: string | null;
  /** Stopped only. */
  reason?: RunReason;
  /** A plain sentence about the reason. */
  detail?: string;
  /** Follow-up notes the live run has not reached yet: the newest `queued` of the user's comments. */
  queued?: number;
  /** Runs this item has had in its lifetime, this one included. */
  count: number;
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
  /** add, personal list only; needs `due`. */
  repeat?: Repeat;
  /** sync and agents */
  on?: boolean;
  /** add: an agent starts on the new item. */
  ask?: boolean;
  /** edit */
  set?: Edit;
  /**
   * Filled in by the hub. edit: the values the fields in `set` held before.
   * done on a repeating item: the due time and checklist before it moved on.
   */
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
  | "ask"
  | "stop"
  | "agents"
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

/** GET /api/settings. */
export interface Settings {
  name: string;
  timeZone: string;
  /** Minutes before an item's due time an early reminder comes, 0 (none) to MAX_LEAD_MINUTES. */
  reminderLead: number;
}

/** PUT /api/settings. A field left out stays as it is. */
export interface SettingsUpdate {
  name?: string;
  timeZone?: string;
  reminderLead?: number;
}

/** GET /api/calendar. */
export interface CalendarStatus {
  on: boolean;
}

/** POST /api/calendar: the feed's address, shown once. */
export interface CalendarLink {
  url: string;
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
