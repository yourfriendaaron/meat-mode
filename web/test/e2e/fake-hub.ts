// An in-memory hub implementing the app API in docs/api.md, served to the page
// through page.route. Fixtures are invented and pinned to NOW.

import type { Page, Route } from "@playwright/test";
import type { Action, Bridge, Item, NewAction, Repo, State } from "../../src/types.ts";

/** Wednesday 14 October 2026, 10:00 UTC. Tests run with timezoneId UTC. */
export const NOW = new Date("2026-10-14T10:00:00Z");

const at = (day: number, h: number, m = 0) => new Date(Date.UTC(2026, 9, day, h, m)).toISOString();

function item(repo: string, id: string, title: string, over: Partial<Item> = {}): Item {
  return {
    key: `home/${repo}/${id}`,
    bridge: "home",
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
      { bridge: "home", name: "personal", personal: true, optedIn: true },
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
      item("personal", "pe-7", "Fix the gate latch", { dueAt: at(16, 9) }),
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
  posted: NewAction[] = [];

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
      user: this.user,
      serverTime: this.now(),
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

    if (method === "GET" && path === "/api/state") return this.json(route, 200, this.state());
    if (method === "GET" && path === "/api/events") {
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 3600000\n\n" });
    }
    if (method === "GET" && path === "/api/actions") {
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
    if (path === "/api/push/subscriptions") return this.json(route, 204);
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

  private postAction(route: Route, a: NewAction) {
    this.posted.push(a);
    const dup = this.log.find((s) => s.action.id === a.id);
    if (dup) return this.json(route, 200, dup.action);
    let bridge = a.bridge ?? "";
    let repo = a.repo ?? "";
    let beadId = "";
    let before: Item | null = null;
    if (a.kind === "add") {
      beadId = `pending-${a.id}`;
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
        pending: true,
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
          (it.comments ??= []).push({ id: `n-${this.seq + 1}`, author: `${this.user}-app`, text: a.payload.text ?? "", createdAt: this.now() });
          break;
      }
      it.pending = true;
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
      status: "pending",
      createdAt: this.now(),
    };
    this.log.push({ action, before });
    return this.json(route, 201, action);
  }
}
