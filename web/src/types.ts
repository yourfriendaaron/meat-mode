// Wire types, mirroring internal/api/types.go. Times are RFC 3339 strings.

/** A file attached to an item of a list the hub keeps (docs/api.md, Attachments). */
export interface Attachment {
  id: string;
  name: string;
  /** The media type the hub sniffed. */
  type: string;
  /** Bytes. */
  size: number;
  sha256: string;
  createdAt: string;
}

/** Caps on attachments, as api.MaxAttachmentBytes and friends. */
export const MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024;
export const MAX_ATTACHMENTS_BYTES = 1024 * 1024 * 1024;
export const MAX_ATTACHMENTS_PER_ITEM = 20;

/** Marks a tracker item for the person; never shown as a tag. */
export const HUMAN_LABEL = "human";
/** Caps on tags, as api.MaxTags and api.MaxTagLen. */
export const MAX_TAGS = 20;
export const MAX_TAG_LEN = 50;

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
  /** Tags; the app never shows HUMAN_LABEL (docs/api.md, Tags). Read-only on a tracker's items. */
  labels?: string[];
  createdBy?: string;
  dueAt?: string | null;
  deferUntil?: string | null;
  createdAt: string;
  updatedAt: string;
  closedAt?: string | null;
  comments: Comment[] | null;
  /** The id of the item this one is a sub-task of, in the same list. */
  parent?: string;
  /** The parent's title as the list's tracker last gave it, for a parent the app does not hold. */
  parentTitle?: string;
  /** Personal items only. */
  repeat?: Repeat | null;
  /** Minutes before the due time an early reminder comes; unset uses the hub's reminderLead. Personal items only. */
  remindBefore?: number | null;
  /** Files on the hub, oldest first. Items of the hub's own lists only. */
  attachments?: Attachment[];
  /** The latest agent run on this item; absent when no agent was ever asked. */
  run?: Run | null;
  pending?: boolean;
  seenAt?: string | null;
  firstSeen?: string;
  error?: string;
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
  repeat?: Repeat;
  /** Ends the repeat; never sent with `repeat`. */
  clearRepeat?: boolean;
  /** Minutes, 0 (no early reminder) to MAX_LEAD_MINUTES. */
  remindBefore?: number;
  /** Goes back to the hub's reminderLead; never sent with `remindBefore`. */
  clearRemindBefore?: boolean;
  /** Moves the item under this parent's id; "" makes it top-level. */
  parent?: string;
  /** Ids of the item's sub-tasks to mark done; the hub fills it into a repeating done's `was`. */
  subtasksDone?: string[];
  /** Replaces the item's tags; [] clears them. */
  labels?: string[];
}

export type RepeatUnit = "day" | "week" | "month" | "year";

/** Caps Repeat.interval. */
export const MAX_REPEAT_INTERVAL = 100;
/** Caps a reminder lead time: one week. */
export const MAX_LEAD_MINUTES = 7 * 24 * 60;

/** One later date of a repeating item's series (docs/api.md, Month view). */
export interface Occurrence {
  key: string;
  at: string;
}

/** GET /api/occurrences: ordered by `at`, then `key`. */
export interface Occurrences {
  occurrences: Occurrence[];
}

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
  /** The repo's task tracker as the app names it, such as "beads" or "Backlog.md"; absent for the hub's own lists. */
  tracker?: string;
  /** Action kinds the tracker can apply, plus "subtask" for an add under a parent. Absent means every kind. */
  actions?: string[];
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
  /** The trackers the machine runs, with their tools' versions. */
  trackers?: TrackerVersion[];
  /** The agent harness the machine runs; absent for an older bridge, which runs Claude Code. */
  harness?: HarnessInfo;
  /** A limits change for this machine waits for its bridge; `harness.limits` shows where it was set to. */
  limitsPending?: boolean;
}

/** The four limits on agent runs on one machine (docs/api.md, Harness). */
export interface AgentLimits {
  /** The spending cap for one run, in US dollars. */
  budgetUsd: number;
  /** The time limit for one run. */
  timeoutSeconds: number;
  /** Runs one task may have in its lifetime. */
  runsPerTask: number;
  /** Runs live at once on the machine. */
  concurrent: number;
}

export type LimitName = keyof AgentLimits;

/** A machine's limits in effect for a run that starts now, its own defaults, and the ceilings the app can set them up to. */
export interface RunLimits extends AgentLimits {
  defaults: AgentLimits;
  max: AgentLimits;
  /** The limits the app has set; the others are the defaults. */
  set?: LimitName[];
}

/** The payload of a limits action: each field set changes that limit, and `reset` puts limits back to the machine's default. */
export interface LimitsChange extends Partial<AgentLimits> {
  reset?: LimitName[];
}

/** The agent harness a machine runs and its limits on each run (docs/harnesses.md, On the wire). */
export interface HarnessInfo {
  /** The registry key, such as "codex": the value of MEAT_MODE_AGENT_HARNESS that picks it. */
  name: string;
  /** The name the app shows, such as "Codex". */
  title?: string;
  version?: string;
  /** The file in a repo the harness reads standing instructions from, such as AGENTS.md. */
  instructionFile?: string;
  /** How the spending cap is enforced; with none a run stops at its time limit only. */
  budget?: "native" | "metered" | "none";
  /** The cap for one run, in US dollars. */
  budgetUsd?: number;
  /** The time limit for one run. */
  timeoutSeconds?: number;
  /** True when a note reaches a live run at once; otherwise it waits for the agent's turn to end. */
  steer?: boolean;
  /** The machine's limits with their defaults and ceilings; absent from a bridge too old for the app to set them. */
  limits?: RunLimits;
}

/** One tracker a machine runs (docs/trackers.md, On the wire). */
export interface TrackerVersion {
  name: string;
  /** The name the app shows, such as "Backlog.md". */
  title?: string;
  version?: string;
  /** The command an agent runs to raise a task for the person. */
  raise?: string;
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
  /** add: the id of the item the new one is a sub-task of. */
  parent?: string;
  /** add: tags, on the hub's own lists only. */
  labels?: string[];
  /** edit */
  set?: Edit;
  /** limits */
  limits?: LimitsChange;
  /**
   * Filled in by the hub. edit: the values the fields in `set` held before.
   * done on a repeating item: the due time before it moved on.
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
  | "limits"
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
  /** The paired machine whose agent posted it, on the hub's own lists; absent for the app's own actions. */
  machine?: string;
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
  /** The latest public DNS check of MEAT_MODE_PUBLIC_URL (docs/api.md, Private network only). */
  publicDns: PublicDns;
  /** Whether this request carried a Tailscale identity header. */
  tailscaleIdentity: boolean;
}

/** What a public resolver says the public address's host resolves to. */
export interface PublicDns {
  state: "private" | "public" | "unchecked" | "skipped";
  /** The global addresses found, for `public` only. */
  addresses?: string[];
  checkedAt: string | null;
}

/** One place reminders go: a browser's push subscription or the Android app (docs/api.md, Devices). */
export interface Device {
  id: string;
  name: string;
  kind: "web" | "app";
  confirmed: boolean;
  createdAt: string;
  lastSeen: string;
  lastPush: string | null;
}

/** Reminders as the Android app reports them over the message bridge (docs/api.md, Android app). */
export type AppPushState = "off" | "waiting" | "on" | "blocked" | "unavailable";

export interface AppPushStatus {
  state: AppPushState;
  endpoint?: string;
  distributor?: string;
  lastPush: string | null;
  batteryExempt: boolean;
}

/** What `push-on` answers: the subscription the page registers with `kind: "app"`. */
export interface AppPushSubscription {
  subscription: { endpoint: string; keys: { p256dh: string; auth: string } };
  name: string;
}
