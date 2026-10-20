// An in-memory hub implementing the app API in docs/api.md, served to the page
// through page.route. Fixtures are invented and pinned to NOW.

import { createHash } from "node:crypto";
import type { Page, Route } from "@playwright/test";
import type { Action, Attachment, Bridge, Device, Edit, HarnessInfo, Item, NewAction, Occurrence, Payload, Repeat, Repo, Run, SetupInfo, SetupStatus, State } from "../../src/types.ts";
import { MAX_ATTACHMENT_BYTES, MAX_ATTACHMENTS_BYTES, MAX_ATTACHMENTS_PER_ITEM } from "../../src/types.ts";

/** Wednesday 14 October 2026, 10:00 UTC. Tests run with timezoneId UTC. */
export const NOW = new Date("2026-10-14T10:00:00Z");

const at = (day: number, h: number, m = 0) => new Date(Date.UTC(2026, 9, day, h, m)).toISOString();

/** The hub keeps the personal list itself, under this reserved bridge name. */
export const HUB = "hub";

function item(repo: string, id: string, title: string, over: Partial<Item> = {}): Item {
  const bridge = repo === "personal" ? HUB : "home";
  return {
    key: `${bridge}/${repo}/${id}`,
    bridge,
    repo,
    id,
    title,
    status: "open",
    priority: 2,
    createdAt: at(10, 9),
    updatedAt: at(10, 9),
    firstSeen: at(10, 9),
    comments: [],
    ...over,
  };
}

/** The hub's tag rules (docs/api.md, Tags): the cleaned list, or the error. */
function cleanLabels(tags: string[]): string[] | string {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const raw of tags) {
    const t = raw.trim();
    if (!t) continue;
    if ([...t].length > 50) return `the tag "${t}" is longer than 50 characters`;
    if (/[\s,\p{Cc}]/u.test(t)) return `the tag "${t}" has a space, comma or control character; a tag is one word`;
    const fold = t.toLowerCase();
    if (fold === "human" || seen.has(fold)) continue;
    seen.add(fold);
    out.push(t);
  }
  return out.length > 20 ? "an item has at most 20 tags" : out;
}

export function fixtures(): { items: Item[]; repos: Repo[]; bridges: Bridge[] } {
  return {
    repos: [
      { bridge: HUB, name: "personal", personal: true, optedIn: true },
      { bridge: "home", name: "garden", personal: false, optedIn: true },
      { bridge: "home", name: "website", personal: false, optedIn: true },
    ],
    bridges: [{ name: "home", createdAt: at(1, 9), lastSeen: at(14, 9, 59), version: "0.1.0", bdVersion: "1.3.0" }],
    items: [
      item("personal", "pe-1", "Water the plants", { dueAt: at(14, 9, 25) }),
      item("personal", "pe-2", "Renew the library card", { dueAt: at(13, 8) }),
      item("personal", "pe-3", "Call Sam about the weekend", { dueAt: at(14, 14, 30) }),
      item("garden", "ga-1", "Order compost for the beds", {
        createdBy: "planner",
        description: "The beds need compost before the autumn planting. Pick a supplier and confirm the delivery date.",
        dueAt: at(14, 18),
        labels: ["human"],
      }),
      item("website", "we-1", "Approve the new footer text", {
        createdBy: "planner",
        description: "The footer changes go live with the next deploy and need a yes or no.",
        dueAt: at(15, 9),
        seenAt: at(14, 8),
        labels: ["human"],
        comments: [
          { id: "c1", author: "planner", text: "The draft is ready for review.", createdAt: at(14, 7) },
          { id: "c2", author: "alex-app", text: "Looks fine, shorten the second line.", createdAt: at(14, 7, 30) },
          { id: "c3", author: "planner", text: "Shortened it. Please approve.", createdAt: at(14, 9) },
        ],
      }),
      item("personal", "pe-4", "Buy seeds", { deferUntil: at(15, 9) }),
      item("personal", "pe-5", "Plan the garden layout", { dueAt: at(23, 9) }),
      item("garden", "ga-2", "Check the tomato labels", { createdBy: "planner", seenAt: at(13, 9), labels: ["human"] }),
      item("personal", "pe-6", "Sweep the porch", { status: "closed", closedAt: at(14, 8, 30), closeReason: "Done in Meat Mode" }),
      item("personal", "pe-7", "Fix the gate latch", { dueAt: at(16, 9), error: "bd: issue is locked" }),
    ],
  };
}

/** The media type the hub would sniff, for the few kinds the tests send. */
function sniff(b: Buffer): string {
  const starts = (sig: number[], at = 0) => sig.every((x, i) => b[at + i] === x);
  if (starts([0x89, 0x50, 0x4e, 0x47])) return "image/png";
  if (starts([0xff, 0xd8, 0xff])) return "image/jpeg";
  if (starts([0x47, 0x49, 0x46, 0x38])) return "image/gif";
  if (starts([0x52, 0x49, 0x46, 0x46]) && starts([0x57, 0x45, 0x42, 0x50], 8)) return "image/webp";
  if (starts([0x25, 0x50, 0x44, 0x46])) return "application/pdf";
  return b.subarray(0, 512).every((x) => x >= 0x20 || x === 9 || x === 10 || x === 13) ? "text/plain" : "application/octet-stream";
}

/** An attachment's name as the hub keeps it (docs/api.md, Attachments). */
function cleanFileName(raw: string): string {
  let name = raw.replace(/\p{Cc}/gu, "").replace(/[/\\]/g, "_").trim();
  while (Buffer.byteLength(name) > 255) name = [...name].slice(0, -1).join("");
  return name || "file";
}

/**
 * Why the hub refuses a repeat (docs/api.md, Repeating to-dos), or "". The
 * real hub's wording differs; the tests look at the status.
 */
function repeatError(r: Repeat): string {
  if (!["day", "week", "month", "year"].includes(r.every)) return "repeat every must be day, week, month or year";
  const n = r.interval ?? 0;
  if (!Number.isInteger(n) || n < 0 || n > 100) return "repeat interval must be 1 to 100";
  const days = r.weekdays ?? [];
  if (days.length && r.every !== "week") return "only a weekly repeat takes weekdays";
  if (days.length && r.fromDone) return "weekdays and fromDone together";
  if (days.some((d) => !Number.isInteger(d) || d < 0 || d > 6) || new Set(days).size !== days.length) return "weekdays must be 0 to 6, each once";
  try {
    if (!r.timeZone) throw new Error();
    new Intl.DateTimeFormat("en", { timeZone: r.timeZone });
  } catch {
    return `unknown repeat time zone "${r.timeZone}"`;
  }
  if (r.until !== undefined && !/^\d{4}-\d{2}-\d{2}$/.test(r.until)) return "repeat until must be a date, YYYY-MM-DD";
  return "";
}

/** Whether a rule's end date falls before the date of a due time. The fake hub counts in UTC. */
function endsBefore(r: Repeat, due: string): boolean {
  return !!r.until && r.until < due.slice(0, 10);
}

/** The hub's copy of a repeat: the client's rule with `start` at the due time. */
function keptRepeat(r: Repeat, due: string): Repeat {
  const out: Repeat = { ...r, start: due };
  if (!out.interval) delete out.interval;
  if (!out.weekdays?.length) delete out.weekdays;
  return out;
}

/** The date after `d` in a rule's series, counted in UTC as nextDue does. */
function stepRepeat(r: Repeat, d: Date): Date {
  const n = r.interval || 1;
  const x = new Date(d);
  if (r.every === "day") x.setUTCDate(x.getUTCDate() + n);
  else if (r.every === "week" && r.weekdays?.length) {
    do x.setUTCDate(x.getUTCDate() + 1);
    while (!r.weekdays.includes(x.getUTCDay()));
  } else if (r.every === "week") x.setUTCDate(x.getUTCDate() + 7 * n);
  else if (r.every === "month") x.setUTCMonth(x.getUTCMonth() + n);
  else x.setUTCFullYear(x.getUTCFullYear() + n);
  return x;
}

/**
 * The later dates of open repeating items in [from, to) (docs/api.md, Month
 * view): after each due time, on or before the rule's end, none for fromDone.
 */
export function occurrencesIn(items: Item[], from: Date, to: Date): Occurrence[] {
  const out: Occurrence[] = [];
  for (const it of items) {
    const r = it.repeat;
    if (it.status === "closed" || !r || !it.dueAt || r.fromDone) continue;
    for (let d = stepRepeat(r, new Date(it.dueAt)); d < to; d = stepRepeat(r, d)) {
      if (r.until && d.toISOString().slice(0, 10) > r.until) break;
      if (d >= from) out.push({ key: it.key, at: d.toISOString() });
    }
  }
  return out.sort((a, b) => a.at.localeCompare(b.at) || a.key.localeCompare(b.key));
}

/**
 * The next occurrence of a repeating item after its due time and now. The
 * fake hub counts in UTC, the zone the tests run in, and a weekly rule with
 * weekdays only every week; tests stay within that.
 */
export function nextDue(r: Repeat, dueAt: string, now: Date): string {
  const step = (d: Date) => stepRepeat(r, d);
  const due = new Date(dueAt);
  if (r.fromDone) {
    const today = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate(), due.getUTCHours(), due.getUTCMinutes()));
    return step(today).toISOString();
  }
  let d = step(due);
  while (d <= now) d = step(d);
  return d.toISOString();
}

/** Kinds that change a personal item only (docs/api.md, Edit rules). */
const EDIT_KINDS: string[] = ["edit", "note-edit", "note-delete", "note-restore"];

interface Stored {
  action: Action;
  before: Item | null; // hub copy before the action, restored on cancel
}

export class FakeHub {
  user = "alex";
  items: Item[];
  repos: Repo[];
  bridges: Bridge[];
  log: Stored[] = [];
  seq = 0;
  /** Last run id the fake bridge gave out. */
  runSeq = 0;
  /** Every request the page made, as "METHOD /path". */
  requests: string[] = [];
  /** When set, GET /api/actions answers 500, as an unreachable hub route would. */
  failActions = false;
  /** When set, GET /api/occurrences answers 500. */
  failOccurrences = false;
  posted: NewAction[] = [];
  /** When set, every /api/events connection sends one change event and asks to reconnect after this many ms. */
  eventRetryMs = 0;
  /**
   * Whether the hub has a password. Off, it serves only its claim step
   * (docs/api.md, Login): every /api/ route but the session ones and the
   * claim answers 401. On, every such route needs a logged-in session.
   */
  login = true;
  password = "correct horse battery staple";
  /** Whether this browser is logged in. */
  authenticated = true;
  /**
   * Whether this session entered the password within the last 10 minutes.
   * Off, POST /api/bridges asks for it again (docs/api.md, Password again).
   */
  passwordFresh = true;
  /** Every body POSTed to /api/bridges. */
  bridgePosts: { name: string; password?: string }[] = [];
  /** When set, POST /api/login answers this status instead of checking the password. */
  loginStatus = 0;
  /**
   * What the hub keeps for GET /api/setup. `status` "" means nothing
   * recorded: the reply then says "new" or "in-use" from the items and
   * machines, as the real hub does. POST /api/setup sets it.
   */
  setup: { status: "" | "done" | "dismissed"; userName: string; timeZone: string; password: SetupInfo["password"] } = {
    status: "",
    userName: "",
    timeZone: "",
    password: "none",
  };
  /** Every body PUT to /api/settings that the hub took in. */
  settingsPuts: { name?: string; timeZone?: string; reminderLead?: number }[] = [];
  /** Minutes before a due time an early reminder comes; 0 is none. */
  reminderLead = 0;
  /** The calendar feed: whether it is on, and the token of its current link. */
  calendar = { on: false, token: "" };
  /** Links POST /api/calendar has made; each token is the next number. */
  calendarLinks = 0;
  /** Push subscriptions the app registered, and the test reminders it asked for. */
  pushSubscriptions = 0;
  /** The devices GET /api/devices lists; a push registration adds one. */
  devices: Device[] = [];
  /** Every body POSTed to /api/push/subscriptions. */
  pushPosts: { endpoint: string; kind?: string; name?: string }[] = [];
  reminderTests = 0;
  /** Endpoints of the push subscriptions the app registered. */
  pushEndpoints = new Set<string>();
  /** The endpoint the last test reminder was asked for. */
  testedEndpoint = "";
  /**
   * Test reminders the push service turns away as gone. Each one removes the
   * subscription, as the hub does, and reports "expired".
   */
  pushGone = 0;
  /** Whether the fake hub reports ntfy as set up in the test reminder. */
  ntfy = false;
  /** When set, GET /api/setup answers after this many ms. */
  setupDelayMs = 0;
  /** The setup code the hub wrote to its log; claiming with it sets the password once. */
  setupCode = "ABCD-EFGH-JKLM";
  setupCodeUsed = false;
  /** When set, POST /api/setup/claim answers this status instead of checking the code. */
  claimStatus = 0;
  /**
   * What GET /api/setup/status says; `login` follows the password. `publicUrl`
   * "" means the address the page was opened at. `time` "" means the hub's
   * clock, which is also the browser's fixed clock; a test sets a time to
   * skew it.
   */
  check: Omit<SetupStatus, "login"> = {
    hostMatches: true,
    publicUrl: "",
    pushKeys: true,
    ntfy: false,
    time: "",
    publicDns: { state: "private", checkedAt: NOW.toISOString() },
    tailscaleIdentity: false,
  };
  /**
   * When set, /api/events carries two change events, the second one a
   * revision later, as the real hub's stream does around a test event.
   * Off, the stream stays silent, as a buffering proxy would leave it.
   */
  liveEvents = false;
  /** Every attachment the hub keeps, removed ones not yet purged included, by id. */
  files = new Map<string, { key: string; attachment: Attachment; bytes: Buffer; removed: boolean }>();
  /** Bytes the hub counts toward its cap besides these files, to bring a test near it. */
  otherFileBytes = 0;
  /** Last attachment id the hub gave out. */
  fileSeq = 0;

  constructor() {
    const f = fixtures();
    this.items = f.items;
    this.repos = f.repos;
    this.bridges = f.bridges;
  }

  now(): string {
    return NOW.toISOString();
  }

  state(): State {
    return {
      items: structuredClone(this.items),
      repos: this.repos,
      bridges: this.bridges,
      unapplied: this.log.filter((s) => s.action.status === "pending").length,
      user: this.setup.userName || this.user,
      serverTime: this.now(),
    };
  }

  /** Makes this a hub fresh from install: the personal list with no items, no machines, no setup recorded. */
  fresh(): void {
    this.items = [];
    this.bridges = [];
    this.repos = this.repos.filter((r) => r.bridge === HUB);
    this.setup.status = "";
  }

  /**
   * What a newly paired bridge does on its first hello: it is seen, says
   * which harness it runs (none for an older bridge), and reports the repos
   * it found, all switched off.
   */
  hello(name: string, repos: string[], harness?: HarnessInfo): void {
    const b = this.bridges.find((x) => x.name === name);
    if (!b) throw new Error(`no machine ${name}`);
    b.lastSeen = this.now();
    b.version = "0.1.0";
    b.bdVersion = "1.3.0";
    b.harness = harness;
    for (const r of repos) this.repos.push({ bridge: name, name: r, personal: false, optedIn: false });
  }

  setupInfo(): SetupInfo {
    const inUse = this.items.length > 0 || this.bridges.length > 0;
    return {
      status: this.setup.status || (inUse ? "in-use" : "new"),
      user: this.setup.userName || this.user,
      timeZone: this.setup.timeZone,
      password: this.passwordSource(),
    };
  }

  /** A hub with a login has a password; one the tests did not set in the app came from the environment. */
  passwordSource(): SetupInfo["password"] {
    if (!this.login) return "none";
    return this.setup.password === "none" ? "env" : this.setup.password;
  }

  settings() {
    return { name: this.setup.userName || this.user, timeZone: this.setup.timeZone, reminderLead: this.reminderLead };
  }

  find(key: string): Item | undefined {
    return this.items.find((i) => i.key === key);
  }

  /** A project the hub keeps, as an agent's first add makes it, with to-dos in it. */
  addProject(name: string, titles: string[]): void {
    this.repos.push({ bridge: HUB, name, personal: false, optedIn: true });
    titles.forEach((title, i) =>
      this.items.push({ ...item("personal", `mm-${90 + i}`, title, { createdBy: "planner" }), key: `${HUB}/${name}/mm-${90 + i}`, repo: name }),
    );
  }

  /** What the bridge does once it applied a limits change: it reports the limits again, no longer pending. */
  applyLimits(name: string): void {
    const b = this.bridges.find((x) => x.name === name);
    if (!b) throw new Error(`no machine ${name}`);
    b.limitsPending = false;
  }

  /** Switches agents on for a repo, as if the bridge had applied the switch. */
  allowAgents(repo: string, on = true): void {
    const r = this.repos.find((x) => x.name === repo && !x.personal);
    if (!r) throw new Error(`no repo ${repo}`);
    r.agentsAllowed = on;
    r.agentsPending = false;
  }

  /**
   * What the bridge's run report does to an item: sets its run, keeping the
   * fields `run` leaves out. A test moves a run between states with it, as
   * the bridge would: `hub.setRun(key, { state: "waiting" })`.
   */
  setRun(key: string, run: Partial<Run> | null): void {
    const it = this.find(key);
    if (!it) throw new Error(`no item ${key}`);
    if (run === null) {
      it.run = null;
      return;
    }
    const was = it.run;
    const next: Run = {
      id: was?.id ?? `run-${++this.runSeq}`,
      state: "working",
      agent: was?.agent ?? it.createdBy ?? "agent",
      startedAt: was?.startedAt ?? this.now(),
      count: was?.count ?? 1,
      ...(was ?? {}),
      ...run,
    };
    if (next.state !== "working") {
      next.queued = 0;
      next.endedAt ??= this.now();
    } else next.endedAt = null;
    if (next.state !== "stopped") delete next.reason;
    it.run = next;
  }

  /**
   * Why the hub refuses an ask on that repo (docs/api.md, Agent runs), or "".
   * A pending switch counts as where it was switched to.
   */
  /**
   * Why the action's list cannot apply it (docs/api.md, Trackers), or "". A
   * list without actions takes every kind; sync, agents, stop and the edits
   * are never refused this way.
   */
  private trackerRefusal(a: NewAction): string {
    const [bridge, repo] = a.key ? [a.key.slice(0, a.key.indexOf("/")), a.key.slice(a.key.indexOf("/") + 1, a.key.lastIndexOf("/"))] : [a.bridge, a.repo];
    const r = this.repos.find((x) => x.bridge === bridge && x.name === repo);
    if (!r?.actions) return "";
    const needs = a.kind === "add" ? [a.payload.parent ? "subtask" : "add", ...(a.payload.ask ? ["ask"] : [])] : ["done", "reopen", "snooze", "dismiss", "note", "ask"].includes(a.kind) ? [a.kind] : [];
    const missing = needs.find((k) => !r.actions!.includes(k));
    return missing ? `${r.tracker ?? "this list's tracker"} cannot ${missing}` : "";
  }

  private askRefusal(bridge: string, repo: string): string {
    if (bridge === HUB) return "the personal list and projects have no agents";
    const r = this.repos.find((x) => x.bridge === bridge && x.name === repo);
    if (!r?.agentsAllowed) return `agents are not allowed in repo "${repo}"; switch them on at its machine: meat-mode bridge agents on ${repo}`;
    return "";
  }

  /**
   * Stores a file on an item as POST /api/items/{key}/attachments does, past
   * no check, and returns it: `hub.attach(key, "fern.png", PNG)`.
   */
  attach(key: string, name: string, bytes: Buffer): Attachment {
    const it = this.find(key);
    if (!it) throw new Error(`no item ${key}`);
    const attachment: Attachment = {
      id: (++this.fileSeq).toString(16).padStart(32, "0"),
      name,
      type: sniff(bytes),
      size: bytes.length,
      sha256: createHash("sha256").update(bytes).digest("hex"),
      createdAt: this.now(),
    };
    this.files.set(attachment.id, { key, attachment, bytes, removed: false });
    it.attachments = [...(it.attachments ?? []), attachment];
    return attachment;
  }

  /** The bytes of every file the hub keeps toward its cap. */
  private fileBytes(): number {
    let n = this.otherFileBytes;
    for (const f of this.files.values()) n += f.bytes.length;
    return n;
  }

  /** The routes of docs/api.md, Attachments, or undefined for another path. */
  private attachmentRoute(route: Route, method: string, path: string, url: URL): Promise<void> | undefined {
    const req = route.request();
    let m = path.match(/^\/api\/items\/([^/]+)\/attachments$/);
    if (method === "POST" && m) {
      const it = this.find(decodeURIComponent(m[1]));
      if (!it) return this.json(route, 404, { error: "no such item" });
      if (it.bridge !== HUB) return this.json(route, 400, { error: "only items of the personal list and projects take attachments" });
      const bytes = req.postDataBuffer() ?? Buffer.alloc(0);
      const name = cleanFileName(url.searchParams.get("name") ?? "");
      const mb = (n: number) => (n / (1 << 20)).toFixed(1);
      if (bytes.length > MAX_ATTACHMENT_BYTES) return this.json(route, 413, { error: `${name} is ${mb(bytes.length)} MB; a file can be at most 25 MB.` });
      if ((it.attachments?.length ?? 0) >= MAX_ATTACHMENTS_PER_ITEM) return this.json(route, 413, { error: "An item can hold at most 20 files." });
      const total = this.fileBytes() + bytes.length;
      if (total > MAX_ATTACHMENTS_BYTES) return this.json(route, 413, { error: `The hub's files would come to ${(total / (1 << 30)).toFixed(2)} GB; they can be at most 1 GB.` });
      return this.json(route, 201, this.attach(it.key, name, bytes));
    }
    m = path.match(/^\/api\/attachments\/([0-9a-f]{32})$/);
    const f = m ? this.files.get(m[1]) : undefined;
    if (method === "GET" && m) {
      if (!f || f.removed) return this.json(route, 404, { error: "no such attachment" });
      const inline = ["image/png", "image/jpeg", "image/gif", "image/webp"].includes(f.attachment.type);
      return route.fulfill({
        status: 200,
        headers: {
          "Content-Type": inline ? f.attachment.type : "application/octet-stream",
          "Content-Disposition": `${inline ? "inline" : "attachment"}; filename*=UTF-8''${encodeURIComponent(f.attachment.name)}`,
          "Content-Security-Policy": "default-src 'none'; sandbox",
          "X-Content-Type-Options": "nosniff",
          "Cache-Control": "private, max-age=86400",
        },
        body: f.bytes,
      });
    }
    if (method === "DELETE" && m) {
      if (!f || f.removed) return this.json(route, 404, { error: "no such attachment" });
      f.removed = true;
      const it = this.find(f.key);
      if (it) it.attachments = (it.attachments ?? []).filter((a) => a.id !== f.attachment.id);
      return this.json(route, 204);
    }
    m = path.match(/^\/api\/attachments\/([0-9a-f]{32})\/restore$/);
    if (method === "POST" && m) {
      const f = this.files.get(m[1]);
      if (!f) return this.json(route, 404, { error: "the attachment was removed for good" });
      const it = this.find(f.key);
      if (f.removed && it) {
        if ((it.attachments?.length ?? 0) >= MAX_ATTACHMENTS_PER_ITEM) return this.json(route, 409, { error: "An item can hold at most 20 files." });
        f.removed = false;
        it.attachments = [...(it.attachments ?? []), f.attachment].sort((a, b) => a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id));
      }
      return this.json(route, 200, f.attachment);
    }
    return undefined;
  }

  /** Purges a removed attachment at once, as the hub's sweep does a day later. */
  purge(id: string): void {
    const f = this.files.get(id);
    if (f?.removed) this.files.delete(id);
  }

  /** What a bridge does: replay every pending action. */
  applyAll(): void {
    for (const s of this.log) {
      if (s.action.status !== "pending") continue;
      s.action.status = "applied";
      s.action.appliedAt = this.now();
    }
    for (const i of this.items) i.pending = false;
  }

  async install(page: Page): Promise<void> {
    await page.route("**/api/**", (route) => this.handle(route));
  }

  private json(route: Route, status: number, body?: unknown) {
    return route.fulfill({
      status,
      contentType: "application/json",
      body: body === undefined ? "" : JSON.stringify(body),
    });
  }

  async handle(route: Route): Promise<void> {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
    const method = req.method();
    this.requests.push(`${method} ${path}`);

    // The real hub refuses a state change without a JSON content type (415);
    // the two routes that take a file take application/octet-stream instead.
    const ctype = (await req.headerValue("content-type")) ?? "";
    const json = /^application\/json(\s*;\s*charset=[^;]+)?$/i.test(ctype);
    const octets = /^application\/octet-stream$/i.test(ctype);
    const upload = method === "POST" && /^\/api\/items\/[^/]+\/attachments$/.test(path);
    const restore = method === "POST" && path === "/api/restore";
    const typeOK = upload ? octets : restore ? json || octets : json;
    if (method !== "GET" && method !== "HEAD" && !typeOK) {
      return this.json(route, 415, { error: `Content-Type must be application/${upload ? "octet-stream" : "json"}` });
    }
    const files = this.attachmentRoute(route, method, path, url);
    if (files) return files;

    if (method === "GET" && path === "/api/session") {
      return this.json(route, 200, { login: this.login, authenticated: this.login && this.authenticated });
    }
    if (method === "POST" && path === "/api/login") {
      if (!this.login) return this.json(route, 404, { error: "this hub has no password yet" });
      if (this.loginStatus) return this.json(route, this.loginStatus, { error: "too many failed logins" });
      const { password } = req.postDataJSON() as { password: string };
      if (password !== this.password) return this.json(route, 401, { error: "wrong password" });
      this.authenticated = true;
      this.passwordFresh = true;
      return this.json(route, 204);
    }
    if (method === "POST" && path === "/api/logout") {
      this.authenticated = false;
      return this.json(route, 204);
    }
    if (method === "POST" && path === "/api/setup/claim") return this.claim(route, req.postDataJSON() as { code: string; password: string });
    if (!this.login) return this.json(route, 401, { error: "this hub has no password yet: set one with the setup code" });
    if (!this.authenticated) return this.json(route, 401, { error: "log in first" });

    if (method === "GET" && path === "/api/state") return this.json(route, 200, this.state());
    if (method === "GET" && path === "/api/setup") {
      if (this.setupDelayMs) await new Promise((r) => setTimeout(r, this.setupDelayMs));
      return this.json(route, 200, this.setupInfo());
    }
    if (method === "POST" && path === "/api/setup") {
      const { status } = req.postDataJSON() as { status: string };
      if (status !== "done" && status !== "dismissed") return this.json(route, 400, { error: "setup status must be done or dismissed" });
      this.setup.status = status;
      return this.json(route, 204);
    }
    if (method === "GET" && path === "/api/settings") return this.json(route, 200, this.settings());
    if (method === "PUT" && path === "/api/settings") {
      const body = req.postDataJSON() as { name?: string; timeZone?: string; reminderLead?: number };
      this.settingsPuts.push(body);
      const lead = body.reminderLead;
      if (lead !== undefined && (!Number.isInteger(lead) || lead < 0 || lead > 10080)) {
        return this.json(route, 400, { error: "reminderLead must be 0 to 10080 minutes" });
      }
      if (body.timeZone !== undefined) {
        try {
          new Intl.DateTimeFormat("en", { timeZone: body.timeZone });
        } catch {
          return this.json(route, 400, { error: `unknown time zone "${body.timeZone}"; use an IANA name such as Europe/Paris` });
        }
        if (!body.timeZone) return this.json(route, 400, { error: "time zone must be an IANA name such as Europe/Paris" });
      }
      if (body.name !== undefined) this.setup.userName = body.name.trim();
      if (body.timeZone !== undefined) this.setup.timeZone = body.timeZone;
      if (lead !== undefined) this.reminderLead = lead;
      return this.json(route, 200, this.settings());
    }
    if (path === "/api/calendar") {
      if (method === "GET") return this.json(route, 200, { on: this.calendar.on });
      if (method === "POST") {
        this.calendar = { on: true, token: `feed-token-${++this.calendarLinks}` };
        return this.json(route, 200, { url: `${url.origin}/calendar/${this.calendar.token}.ics` });
      }
      if (method === "DELETE") {
        this.calendar = { on: false, token: "" };
        return this.json(route, 204);
      }
    }
    if (method === "PUT" && path === "/api/password") {
      const { current, password } = req.postDataJSON() as { current: string; password: string };
      if (this.setup.password !== "app") return this.json(route, 409, { error: "this hub has no password set in the app" });
      if (password.length < 8) return this.json(route, 400, { error: "the password must be at least 8 characters" });
      if (current !== this.password) return this.json(route, 403, { error: "wrong password" });
      this.password = password;
      this.passwordFresh = true;
      return this.json(route, 204);
    }
    if (method === "GET" && path === "/api/setup/status") {
      const login = { env: "environment", app: "app", none: "none" } as const;
      return this.json(route, 200, {
        ...this.check,
        login: login[this.passwordSource()],
        publicUrl: this.check.publicUrl || url.origin,
        time: this.check.time || this.now(),
      } satisfies SetupStatus);
    }
    if (method === "POST" && path === "/api/setup/test-event") return this.json(route, 204);
    if (method === "GET" && path === "/api/events" && this.liveEvents) {
      const body = `retry: 3600000\n\nevent: change\ndata: {"rev": ${this.seq}}\n\nevent: change\ndata: {"rev": ${this.seq + 1}}\n\n`;
      return route.fulfill({ status: 200, contentType: "text/event-stream", body });
    }
    if (method === "GET" && path === "/api/events") {
      const body = this.eventRetryMs ? `retry: ${this.eventRetryMs}\n\nevent: change\ndata: {"rev":${this.seq}}\n\n` : "retry: 3600000\n\n";
      return route.fulfill({ status: 200, contentType: "text/event-stream", body });
    }
    if (method === "GET" && path === "/api/actions") {
      if (this.failActions) return this.json(route, 500, { error: "actions unavailable" });
      const limit = Number(url.searchParams.get("limit") || 50);
      return this.json(
        route,
        200,
        [...this.log].reverse().slice(0, limit).map((s) => s.action),
      );
    }
    if (method === "POST" && path === "/api/actions") {
      return this.postAction(route, req.postDataJSON() as NewAction);
    }
    if (method === "GET" && path === "/api/occurrences") {
      if (this.failOccurrences) return this.json(route, 500, { error: "occurrences unavailable" });
      const from = new Date(url.searchParams.get("from") ?? "");
      const to = new Date(url.searchParams.get("to") ?? "");
      if (Number.isNaN(from.getTime()) || Number.isNaN(to.getTime())) return this.json(route, 400, { error: "from and to must be RFC 3339 times" });
      if (to <= from) return this.json(route, 400, { error: "to must be after from" });
      if (to.getTime() - from.getTime() > 62 * 24 * 60 * 60 * 1000) return this.json(route, 400, { error: "the window is longer than 62 days" });
      return this.json(route, 200, { occurrences: occurrencesIn(this.items, from, to) });
    }
    let m = path.match(/^\/api\/actions\/([^/]+)\/cancel$/);
    if (method === "POST" && m) {
      const s = this.log.find((x) => x.action.id === decodeURIComponent(m![1]));
      if (!s) return this.json(route, 404, { error: "no such action" });
      if (s.action.status !== "pending") return this.json(route, 409, { error: "already applied" });
      s.action.status = "cancelled";
      const key = `${s.action.bridge}/${s.action.repo}/${s.action.beadId}`;
      const idx = this.items.findIndex((i) => i.key === key);
      if (s.before && idx >= 0) this.items[idx] = s.before;
      return this.json(route, 200, s.action);
    }
    m = path.match(/^\/api\/items\/([^/]+)\/seen$/);
    if (method === "POST" && m) {
      const it = this.find(decodeURIComponent(m[1]));
      if (it) it.seenAt = this.now();
      return this.json(route, 204);
    }
    if (method === "GET" && path === "/api/push/key") return this.json(route, 200, { publicKey: "BExampleKeyOnlyForTests" });
    if (path === "/api/push/subscriptions") {
      if (method === "POST") {
        const body = req.postDataJSON() as { endpoint: string; kind?: string; name?: string };
        this.pushSubscriptions++;
        this.pushEndpoints.add(body.endpoint);
        this.pushPosts.push(body);
        const kind = body.kind === "app" ? "app" : "web";
        const id = String(this.devices.length + 1).padStart(32, "0");
        const at = this.now();
        this.devices.unshift({ id, name: body.name || (kind === "app" ? "Android app" : "Browser"), kind, confirmed: kind === "web", createdAt: at, lastSeen: at, lastPush: null });
        // An app registration waits for its confirmation (docs/api.md, Devices).
        if (kind === "app") return this.json(route, 202, { device: id });
      }
      return this.json(route, 204);
    }
    if (method === "POST" && path === "/api/reminders/test") {
      this.reminderTests++;
      const { endpoint } = req.postDataJSON() as { endpoint: string };
      this.testedEndpoint = endpoint;
      const ntfy = this.ntfy ? "sent" : "off";
      const known = this.pushSubscriptions > 0 && this.pushEndpoints.has(endpoint);
      if (known && this.pushGone > 0) {
        this.pushGone--;
        this.pushSubscriptions--;
        this.pushEndpoints.delete(endpoint);
        return this.json(route, 200, { push: "expired", ntfy });
      }
      return this.json(route, 200, { push: known ? "sent" : "none", ntfy });
    }
    if (method === "GET" && path === "/api/devices") return this.json(route, 200, this.devices);
    m = path.match(/^\/api\/devices\/([0-9a-f]{32})$/);
    if (method === "DELETE" && m) {
      if (!this.devices.some((d) => d.id === m![1])) return this.json(route, 404, { error: "no such device" });
      this.devices = this.devices.filter((d) => d.id !== m![1]);
      return this.json(route, 204);
    }
    if (method === "GET" && path === "/api/bridges") return this.json(route, 200, this.bridges);
    if (method === "POST" && path === "/api/bridges") {
      const body = req.postDataJSON() as { name: string; password?: string };
      this.bridgePosts.push(body);
      const { name, password } = body;
      if (!this.passwordFresh) {
        if (password === undefined) return this.json(route, 403, { error: "enter your password to add a machine", reauth: true });
        // Wrong passwords here count with wrong logins.
        if (this.loginStatus) return route.fulfill({ status: this.loginStatus, contentType: "application/json", headers: { "Retry-After": "60" }, body: JSON.stringify({ error: "too many failed logins" }) });
        if (password !== this.password) return this.json(route, 403, { error: "wrong password", reauth: true });
        this.passwordFresh = true;
      }
      if (this.bridges.some((b) => b.name === name)) return this.json(route, 409, { error: "a machine with that name exists" });
      this.bridges.push({ name, createdAt: this.now() });
      return this.json(route, 201, { name, token: "test-token" });
    }
    m = path.match(/^\/api\/bridges\/([^/]+)$/);
    if (method === "DELETE" && m) {
      this.bridges = this.bridges.filter((b) => b.name !== decodeURIComponent(m![1]));
      return this.json(route, 204);
    }
    return this.json(route, 404, { error: "not found" });
  }

  /** POST /api/setup/claim: sets the password with the setup code and logs this browser in. */
  private claim(route: Route, body: { code: string; password: string }) {
    if (this.claimStatus) return this.json(route, this.claimStatus, { error: "refused" });
    const has = this.setupInfo().password;
    if (has === "env") return this.json(route, 409, { error: "the password is set by the hub's environment" });
    if (has === "app") return this.json(route, 409, { error: "this hub already has a password" });
    if (body.password.length < 8) return this.json(route, 400, { error: "the password must be at least 8 characters" });
    const norm = (c: string) => c.replace(/[-\s]/g, "").toUpperCase();
    if (this.setupCodeUsed || norm(body.code) !== norm(this.setupCode)) {
      return this.json(route, 403, { error: "the setup code is wrong, expired or used" });
    }
    this.setupCodeUsed = true;
    this.setup.password = "app";
    this.password = body.password;
    this.login = true;
    this.authenticated = true;
    this.passwordFresh = true;
    return this.json(route, 204);
  }

  /**
   * Applies edit, note-edit, note-delete or note-restore to a personal item
   * as docs/api.md says, and returns the payload with `was` or `comment`
   * filled in, or the reason it is refused.
   */
  private applyEdit(it: Item, kind: string, p: Payload): Payload | { status: number; error: string } {
    const bad = (error: string, status = 400) => ({ status, error });
    if (kind === "edit") {
      const set = p.set;
      if (!set || !Object.keys(set).length) return bad("edit needs at least one field in payload.set");
      if (set.due && set.clearDue) return bad("due and clearDue together");
      if (set.repeat && set.clearRepeat) return bad("repeat and clearRepeat together");
      if (set.remindBefore !== undefined && set.clearRemindBefore) return bad("remindBefore and clearRemindBefore together");
      if (set.remindBefore !== undefined && (!Number.isInteger(set.remindBefore) || set.remindBefore < 0 || set.remindBefore > 10080)) {
        return bad("remindBefore must be 0 to 10080 minutes");
      }
      if (set.repeat) {
        const why = repeatError(set.repeat);
        if (why) return bad(why);
        if (set.clearDue || !(set.due ?? it.dueAt)) return bad("a repeat needs a due date");
      }
      const subs = this.items.filter((i) => i.parent === it.id && i.bridge === it.bridge && i.repo === it.repo);
      if (set.parent) {
        // A move follows the add rules, and an item with sub-tasks stays top-level (docs/api.md, Sub-tasks).
        if (set.parent === it.id) return bad("an item cannot be its own parent");
        const p = this.find(`${it.bridge}/${it.repo}/${set.parent}`);
        if (!p) return bad("no such parent", 404);
        if (p.status === "closed" || p.parent) return bad("not a parent");
        if (subs.length) return bad("an item with sub-tasks cannot become a sub-task");
      }
      if (set.subtasksDone?.some((id) => !subs.some((i) => i.id === id))) return bad("subtasksDone names an item that is not its sub-task");
      const rule = set.repeat ?? (set.clearRepeat ? null : it.repeat);
      const at = set.due ?? it.dueAt;
      if (rule && at && !set.clearDue && endsBefore(rule, at)) return bad("repeat until is before the due date");
      const was: Edit = {};
      if (set.title !== undefined) {
        const t = set.title.trim();
        if (!t || t.length > 1000) return bad("title must be 1 to 1000 characters");
        was.title = it.title;
        it.title = t;
      }
      if (set.description !== undefined) {
        if (set.description.length > 10000) return bad("description too long");
        was.description = it.description ?? "";
        it.description = set.description;
      }
      if (set.due !== undefined || set.clearDue) {
        if (it.dueAt) was.due = it.dueAt;
        else was.clearDue = true;
        it.dueAt = set.clearDue ? null : set.due;
        // Clearing the due date ends the repeat; moving it moves the series.
        if (set.clearDue && it.repeat && !set.clearRepeat) {
          was.repeat = it.repeat;
          it.repeat = null;
        }
        if (it.repeat && it.dueAt) it.repeat = keptRepeat(it.repeat, it.dueAt);
      }
      if (set.repeat || set.clearRepeat) {
        if (it.repeat) was.repeat = it.repeat;
        else was.clearRepeat = true;
        it.repeat = set.repeat ? keptRepeat(set.repeat, it.dueAt!) : null;
      }
      if (set.remindBefore !== undefined || set.clearRemindBefore) {
        if (it.remindBefore != null) was.remindBefore = it.remindBefore;
        else was.clearRemindBefore = true;
        it.remindBefore = set.clearRemindBefore ? null : set.remindBefore;
      }
      if (set.priority !== undefined) {
        if (set.priority < 0 || set.priority > 4) return bad("priority must be 0 to 4");
        was.priority = it.priority;
        it.priority = set.priority;
      }
      if (set.labels !== undefined) {
        const clean = cleanLabels(set.labels);
        if (typeof clean === "string") return bad(clean);
        was.labels = [...(it.labels ?? [])];
        it.labels = clean.length ? clean : undefined;
      }
      if (set.parent !== undefined) {
        was.parent = it.parent ?? "";
        it.parent = set.parent || undefined;
      }
      for (const s of subs) {
        if (!set.subtasksDone?.includes(s.id) || s.status === "closed") continue;
        s.status = "closed";
        s.closeReason = "Done in Meat Mode";
        s.closedAt = this.now();
      }
      return { set, was };
    }
    const comments = (it.comments ??= []);
    if (kind === "note-restore") {
      const c = p.comment;
      if (!c) return bad("note-restore needs comment");
      if (comments.some((x) => x.id === c.id)) return bad(`note ${c.id} is already on this item`, 409);
      const at = comments.findIndex((x) => x.createdAt > c.createdAt);
      comments.splice(at < 0 ? comments.length : at, 0, structuredClone(c));
      return p;
    }
    const idx = comments.findIndex((c) => c.id === p.commentId);
    if (idx < 0) return bad(`no note ${p.commentId} on this item`, 404);
    const comment = structuredClone(comments[idx]);
    if (kind === "note-edit") {
      const text = (p.text ?? "").trim();
      if (!text || text.length > 10000) return bad("note must be 1 to 10000 characters");
      comments[idx].text = text;
    } else {
      comments.splice(idx, 1);
    }
    return { ...p, comment };
  }

  private postAction(route: Route, a: NewAction) {
    this.posted.push(a);
    const dup = this.log.find((s) => s.action.id === a.id);
    if (dup) return this.json(route, 200, dup.action);
    const sent = a.payload as { checklist?: unknown; set?: { checklist?: unknown } };
    if (sent.checklist !== undefined || sent.set?.checklist !== undefined) {
      return this.json(route, 400, { error: "Checklists are now sub-tasks: add each entry as a sub-task instead (docs/api.md, Sub-tasks)." });
    }
    const cannot = this.trackerRefusal(a);
    if (cannot) return this.json(route, 400, { error: cannot });
    let bridge = a.bridge ?? "";
    let repo = a.repo ?? "";
    let beadId = "";
    let before: Item | null = null;
    let payload: Payload = a.payload;
    // The hub applies an action on its own lists at once.
    const onHub = (a.bridge ?? a.key?.split("/")[0]) === HUB;
    if (EDIT_KINDS.includes(a.kind)) {
      const it = a.key ? this.find(a.key) : undefined;
      if (!it) return this.json(route, 404, { error: "no such item" });
      if (!onHub) return this.json(route, 400, { error: "an agent's item changes through a note" });
      const res = this.applyEdit(it, a.kind, a.payload);
      if ("status" in res) return this.json(route, res.status, { error: res.error });
      payload = res;
      bridge = it.bridge;
      repo = it.repo;
      beadId = it.id;
    } else if (a.kind === "sync") {
      const r = this.repos.find((x) => x.bridge === bridge && x.name === repo);
      if (!r || r.bridge === HUB) return this.json(route, 400, { error: "no such repo" });
      r.optedIn = Boolean(a.payload.on);
      r.pending = true;
    } else if (a.kind === "limits") {
      const b = this.bridges.find((x) => x.name === bridge);
      if (!b || bridge === HUB) return this.json(route, 400, { error: "the personal list and projects have no agents" });
      const l = b.harness?.limits;
      if (!l) return this.json(route, 400, { error: `the bridge on ${bridge} is too old to take limits from the app; update it, or set MEAT_MODE_AGENT_* there` });
      const c = a.payload.limits ?? {};
      // The hub refuses a value above the ceiling the bridge reported (docs/api.md, Harness).
      for (const n of ["budgetUsd", "timeoutSeconds", "runsPerTask", "concurrent"] as const) {
        const v = c[n];
        const what = { budgetUsd: "the spending cap", timeoutSeconds: "the time limit", runsPerTask: "runs per task", concurrent: "runs at once" }[n];
        if (v !== undefined && v > l.max[n]) return this.json(route, 400, { error: `${what} can be at most ${l.max[n]} on ${bridge}` });
      }
      const set = new Set(l.set ?? []);
      for (const n of c.reset ?? []) {
        l[n] = l.defaults[n];
        set.delete(n);
      }
      for (const n of ["budgetUsd", "timeoutSeconds", "runsPerTask", "concurrent"] as const) {
        const v = c[n];
        if (v === undefined) continue;
        l[n] = v;
        set.add(n);
      }
      l.set = [...set];
      if (b.harness) {
        b.harness.timeoutSeconds = l.timeoutSeconds;
        if (b.harness.budget !== "none") b.harness.budgetUsd = l.budgetUsd;
      }
      b.limitsPending = true;
    } else if (a.kind === "agents") {
      const r = this.repos.find((x) => x.bridge === bridge && x.name === repo);
      if (!r || r.bridge === HUB) return this.json(route, 400, { error: "no such repo" });
      // Agents switch on only at the agent machine (docs/api.md, Agents).
      if (a.payload.on) return this.json(route, 400, { error: `switch agents on at this machine: meat-mode bridge agents on ${repo}` });
      r.agentsAllowed = false;
      r.agentsPending = true;
    } else if (a.kind === "add") {
      if (a.payload.ask) {
        const why = this.askRefusal(bridge, repo);
        if (why) return this.json(route, 400, { error: why });
      }
      if (a.payload.parent) {
        // A sub-task's parent is open, top-level and in the same list (docs/api.md, Sub-tasks).
        const p = this.find(`${bridge}/${repo}/${a.payload.parent}`);
        if (!p) return this.json(route, 404, { error: "no such parent" });
        if (p.status === "closed" || p.parent) return this.json(route, 400, { error: "not a parent" });
      }
      const r = a.payload.repeat;
      if (r) {
        if (!onHub) return this.json(route, 400, { error: "only the personal list and projects take a repeat" });
        if (!a.payload.due) return this.json(route, 400, { error: "a repeat needs a due date" });
        const why = repeatError(r);
        if (why) return this.json(route, 400, { error: why });
        if (endsBefore(r, a.payload.due)) return this.json(route, 400, { error: "repeat until is before the due date" });
      }
      let labels: string[] | undefined;
      if (a.payload.labels) {
        if (!onHub) return this.json(route, 400, { error: "only the personal list and projects take tags" });
        const clean = cleanLabels(a.payload.labels);
        if (typeof clean === "string") return this.json(route, 400, { error: clean });
        labels = clean;
      }
      beadId = onHub ? `mm-${this.seq + 1}` : `pending-${a.id}`;
      this.items.push({
        key: `${bridge}/${repo}/${beadId}`,
        bridge,
        repo,
        id: beadId,
        title: a.payload.title ?? "",
        status: "open",
        priority: 2,
        dueAt: a.payload.due ?? null,
        ...(a.payload.parent ? { parent: a.payload.parent } : {}),
        ...(r ? { repeat: keptRepeat(r, a.payload.due!) } : {}),
        ...(labels?.length ? { labels } : {}),
        createdAt: this.now(),
        updatedAt: this.now(),
        firstSeen: this.now(),
        seenAt: this.now(),
        comments: [],
        pending: !onHub,
        // With ask, the provisional item shows the run starting at once.
        ...(a.payload.ask ? { run: { id: `pending-${a.id}`, state: "working" as const, agent: "agent", activity: "Starting", startedAt: this.now(), count: 1 } } : {}),
      });
    } else {
      const it = a.key ? this.find(a.key) : undefined;
      if (!it) return this.json(route, 404, { error: "no such item" });
      if (a.kind === "ask") {
        const why = this.askRefusal(it.bridge, it.repo);
        if (why) return this.json(route, 400, { error: why });
      }
      if ((a.kind === "done" || a.kind === "dismiss") && this.items.some((i) => i.parent === it.id && i.repo === it.repo && i.bridge === it.bridge && i.status !== "closed")) {
        return this.json(route, 409, { error: "finish or remove its open sub-tasks first" });
      }
      before = structuredClone(it);
      bridge = it.bridge;
      repo = it.repo;
      beadId = it.id;
      switch (a.kind) {
        case "done":
          // A repeating item moves on and stays open (docs/api.md, Repeating to-dos),
          // until its next date falls after the rule's end.
          if (it.repeat && it.dueAt && onHub && !endsBefore(it.repeat, nextDue(it.repeat, it.dueAt, NOW))) {
            const was: Edit = { due: it.dueAt };
            // Its sub-tasks closed as done open again; dismissed ones stay closed.
            const reopened = this.items.filter((i) => i.parent === it.id && i.bridge === it.bridge && i.repo === it.repo && i.status === "closed" && i.closeReason === "Done in Meat Mode");
            for (const s of reopened) {
              s.status = "open";
              s.closedAt = null;
              s.closeReason = undefined;
            }
            if (reopened.length) was.subtasksDone = reopened.map((s) => s.id);
            payload = { ...a.payload, was };
            it.dueAt = nextDue(it.repeat, it.dueAt, NOW);
            it.deferUntil = null;
            break;
          }
          it.status = "closed";
          it.closeReason = "Done in Meat Mode";
          it.closedAt = this.now();
          break;
        case "dismiss":
          it.status = "closed";
          it.closeReason = "Dismissed";
          it.repeat = null;
          it.closedAt = this.now();
          break;
        case "reopen":
          it.status = "open";
          it.closedAt = null;
          it.closeReason = undefined;
          break;
        case "snooze":
          it.deferUntil = a.payload.until ?? null;
          break;
        case "note":
          (it.comments ??= []).push({ id: onHub ? a.id : `n-${this.seq + 1}`, author: `${this.user}-app`, text: a.payload.text ?? "", createdAt: this.now() });
          break;
        case "ask": {
          // The note goes first, as note does; then the run shows working at
          // once, or a live run counts the note as queued.
          const text = a.payload.text?.trim();
          if (text) (it.comments ??= []).push({ id: `n-${this.seq + 1}`, author: `${this.user}-app`, text, createdAt: this.now() });
          const was = it.run;
          if (was?.state === "working") {
            if (text) was.queued = (was.queued ?? 0) + 1;
          } else {
            it.run = {
              id: `pending-${a.id}`,
              state: "working",
              agent: was?.agent || it.createdBy || "agent",
              activity: "Starting",
              startedAt: this.now(),
              count: (was?.count ?? 0) + 1,
            };
          }
          break;
        }
        case "stop":
          if (it.run?.state === "working") {
            it.run = { ...it.run, state: "stopped", reason: "stopped", endedAt: this.now(), queued: 0 };
          }
          break;
      }
      it.pending = !onHub;
    }
    const action: Action = {
      id: a.id,
      seq: ++this.seq,
      kind: a.kind,
      bridge,
      repo,
      beadId,
      payload,
      author: `${this.user}-app`,
      status: onHub ? "applied" : "pending",
      createdAt: this.now(),
      ...(onHub ? { appliedAt: this.now() } : {}),
    };
    this.log.push({ action, before });
    return this.json(route, 201, action);
  }
}
