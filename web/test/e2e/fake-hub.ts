// An in-memory hub implementing the app API in docs/api.md, served to the page
// through page.route. Fixtures are invented and pinned to NOW.

import type { Page, Route } from "@playwright/test";
import type { Action, Bridge, Item, NewAction, Repo, SetupInfo, SetupStatus, State } from "../../src/types.ts";

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
  /** Every request the page made, as "METHOD /path". */
  requests: string[] = [];
  /** When set, GET /api/actions answers 500, as an unreachable hub route would. */
  failActions = false;
  posted: NewAction[] = [];
  /** When set, every /api/events connection sends one change event and asks to reconnect after this many ms. */
  eventRetryMs = 0;
  /** When set, the hub has a login: every /api/ route but the session ones needs a logged-in session. */
  login = false;
  password = "correct horse battery staple";
  authenticated = false;
  /** When set, POST /api/login answers this status instead of checking the password. */
  loginStatus = 0;
  /**
   * What the hub keeps for GET /api/setup. `status` "" means nothing
   * recorded: the reply then says "new" or "in-use" from the items and
   * machines, as the real hub does. POST /api/setup sets it.
   */
  setup: { status: "" | "done" | "dismissed"; userName: string; timeZone: string; password: SetupInfo["password"]; tailscaleOnly: boolean } = {
    status: "",
    userName: "",
    timeZone: "",
    password: "none",
    tailscaleOnly: false,
  };
  /** Every body PUT to /api/settings that the hub took in. */
  settingsPuts: { name?: string; timeZone?: string }[] = [];
  /** Push subscriptions the app registered, and the test reminders it asked for. */
  pushSubscriptions = 0;
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
   * What GET /api/setup/status says. `publicUrl` "" means the address the
   * page was opened at. `time` "" means the hub's clock, which is also the
   * browser's fixed clock; a test sets a time to skew it.
   */
  check: { login: SetupStatus["login"]; hostMatches: boolean; publicUrl: string; pushKeys: boolean; ntfy: boolean; time: string } = {
    login: "none",
    hostMatches: true,
    publicUrl: "",
    pushKeys: true,
    ntfy: false,
    time: "",
  };
  /**
   * When set, /api/events carries two change events, the second one a
   * revision later, as the real hub's stream does around a test event.
   * Off, the stream stays silent, as a buffering proxy would leave it.
   */
  liveEvents = false;

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

  /** What a newly paired bridge does on its first hello: it is seen, and reports the repos it found, all switched off. */
  hello(name: string, repos: string[]): void {
    const b = this.bridges.find((x) => x.name === name);
    if (!b) throw new Error(`no machine ${name}`);
    b.lastSeen = this.now();
    b.version = "0.1.0";
    b.bdVersion = "1.3.0";
    for (const r of repos) this.repos.push({ bridge: name, name: r, personal: false, optedIn: false });
  }

  setupInfo(): SetupInfo {
    const inUse = this.items.length > 0 || this.bridges.length > 0;
    return {
      status: this.setup.status || (inUse ? "in-use" : "new"),
      user: this.setup.userName || this.user,
      timeZone: this.setup.timeZone,
      // A hub with a login has a password; one the tests did not set in the app came from the environment.
      password: this.login && this.setup.password === "none" ? "env" : this.setup.password,
      tailscaleOnly: this.setup.tailscaleOnly,
    };
  }

  find(key: string): Item | undefined {
    return this.items.find((i) => i.key === key);
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

    // The real hub refuses a state change without a JSON content type (415).
    const ctype = (await req.headerValue("content-type")) ?? "";
    if (method !== "GET" && method !== "HEAD" && !/^application\/json(\s*;\s*charset=[^;]+)?$/i.test(ctype)) {
      return this.json(route, 415, { error: "Content-Type must be application/json" });
    }

    if (method === "GET" && path === "/api/session") {
      return this.json(route, 200, { login: this.login, authenticated: !this.login || this.authenticated });
    }
    if (method === "POST" && path === "/api/login") {
      if (!this.login) return this.json(route, 404, { error: "this hub has no login" });
      if (this.loginStatus) return this.json(route, this.loginStatus, { error: "too many failed logins" });
      const { password } = req.postDataJSON() as { password: string };
      if (password !== this.password) return this.json(route, 401, { error: "wrong password" });
      this.authenticated = true;
      return this.json(route, 204);
    }
    if (method === "POST" && path === "/api/logout") {
      this.authenticated = false;
      return this.json(route, 204);
    }
    if (method === "POST" && path === "/api/setup/claim") return this.claim(route, req.postDataJSON() as { code: string; password: string });
    if (this.login && !this.authenticated) return this.json(route, 401, { error: "log in first" });

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
    if (method === "GET" && path === "/api/settings") {
      return this.json(route, 200, { name: this.setup.userName || this.user, timeZone: this.setup.timeZone });
    }
    if (method === "PUT" && path === "/api/settings") {
      const body = req.postDataJSON() as { name?: string; timeZone?: string };
      this.settingsPuts.push(body);
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
      return this.json(route, 200, { name: this.setup.userName || this.user, timeZone: this.setup.timeZone });
    }
    if (method === "PUT" && path === "/api/password") {
      const { current, password } = req.postDataJSON() as { current: string; password: string };
      if (this.setup.password !== "app") return this.json(route, 409, { error: "this hub has no password set in the app" });
      if (password.length < 8) return this.json(route, 400, { error: "the password must be at least 8 characters" });
      if (current !== this.password) return this.json(route, 403, { error: "wrong password" });
      this.password = password;
      return this.json(route, 204);
    }
    if (method === "PUT" && path === "/api/setup/tailscale-only") {
      const { on } = req.postDataJSON() as { on: boolean };
      if (on && this.setupInfo().password !== "none") return this.json(route, 409, { error: "this hub has a password" });
      this.setup.tailscaleOnly = on;
      return this.json(route, 204);
    }
    if (method === "GET" && path === "/api/setup/status") {
      return this.json(route, 200, { ...this.check, publicUrl: this.check.publicUrl || url.origin, time: this.check.time || this.now() });
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
        this.pushSubscriptions++;
        this.pushEndpoints.add((req.postDataJSON() as { endpoint: string }).endpoint);
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
    if (method === "GET" && path === "/api/bridges") return this.json(route, 200, this.bridges);
    if (method === "POST" && path === "/api/bridges") {
      const { name } = req.postDataJSON() as { name: string };
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
    this.setup.tailscaleOnly = false;
    this.password = body.password;
    this.login = true;
    this.authenticated = true;
    return this.json(route, 204);
  }

  private postAction(route: Route, a: NewAction) {
    this.posted.push(a);
    const dup = this.log.find((s) => s.action.id === a.id);
    if (dup) return this.json(route, 200, dup.action);
    let bridge = a.bridge ?? "";
    let repo = a.repo ?? "";
    let beadId = "";
    let before: Item | null = null;
    // The hub applies an action on its own personal list at once.
    const onHub = (a.bridge ?? a.key?.split("/")[0]) === HUB;
    if (a.kind === "sync") {
      const r = this.repos.find((x) => x.bridge === bridge && x.name === repo);
      if (!r || r.personal) return this.json(route, 400, { error: "no such repo" });
      r.optedIn = Boolean(a.payload.on);
      r.pending = true;
    } else if (a.kind === "add") {
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
        createdAt: this.now(),
        updatedAt: this.now(),
        firstSeen: this.now(),
        seenAt: this.now(),
        comments: [],
        pending: !onHub,
      });
    } else {
      const it = a.key ? this.find(a.key) : undefined;
      if (!it) return this.json(route, 404, { error: "no such item" });
      before = structuredClone(it);
      bridge = it.bridge;
      repo = it.repo;
      beadId = it.id;
      switch (a.kind) {
        case "done":
          it.status = "closed";
          it.closeReason = "Done in Meat Mode";
          it.closedAt = this.now();
          break;
        case "dismiss":
          it.status = "closed";
          it.closeReason = "Dismissed";
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
      payload: a.payload,
      author: `${this.user}-app`,
      status: onHub ? "applied" : "pending",
      createdAt: this.now(),
      ...(onHub ? { appliedAt: this.now() } : {}),
    };
    this.log.push({ action, before });
    return this.json(route, 201, action);
  }
}
