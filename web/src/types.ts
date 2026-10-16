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
}

export type ActionKind = "done" | "reopen" | "snooze" | "dismiss" | "note" | "add";

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
