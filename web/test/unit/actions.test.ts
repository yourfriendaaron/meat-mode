import { test } from "node:test";
import assert from "node:assert/strict";
import { ApiError } from "../../src/api.ts";
import { createActions, movedOn, type ActionsApi } from "../../src/actions.ts";
import type { Action, Item, NewAction } from "../../src/types.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const due = new Date(2026, 9, 14, 9, 0).toISOString();
const nextDue = new Date(2026, 9, 15, 9, 0).toISOString();

function item(key: string, over: Partial<Item> = {}): Item {
  const [bridge, repo, id] = key.split("/");
  return {
    key,
    bridge,
    repo,
    id,
    title: `Task ${id}`,
    status: "open",
    priority: 2,
    createdAt: now.toISOString(),
    updatedAt: now.toISOString(),
    comments: [],
    ...over,
  };
}

/**
 * A hub in memory: it applies personal actions at once and leaves agent ones
 * pending, moves a repeating personal item on with `was` in its reply, and
 * refuses to cancel an applied action with 409.
 */
function fakeHub(items: Item[], opts: { failPost?: (a: NewAction) => boolean; failCancel?: boolean } = {}) {
  const posted: NewAction[] = [];
  const cancelled: string[] = [];
  const applied = new Set<string>();
  const api: ActionsApi = {
    async postAction(a) {
      if (opts.failPost?.(a)) throw new ApiError(500, "hub down");
      posted.push(a);
      const it = items.find((i) => i.key === a.key);
      const personal = it?.bridge === "hub";
      if (personal) applied.add(a.id);
      const payload = { ...a.payload };
      if (a.kind === "done" && personal && it?.repeat) payload.was = { due: it.dueAt ?? undefined };
      const [bridge, repo, beadId] = (a.key ?? "").split("/");
      const reply: Action = {
        id: a.id,
        seq: posted.length,
        kind: a.kind,
        bridge,
        repo,
        beadId,
        payload,
        author: "alex-app",
        status: personal ? "applied" : "pending",
        createdAt: now.toISOString(),
      };
      return reply;
    },
    async cancelAction(id) {
      if (opts.failCancel) throw new ApiError(500, "hub down");
      if (applied.has(id)) throw new ApiError(409, "already applied");
      cancelled.push(id);
    },
  };
  const errors: string[] = [];
  let n = 0;
  const actions = createActions({
    api,
    newId: () => `id-${++n}`,
    now: () => now,
    item: (key) => items.find((i) => i.key === key),
    onError: (during, e) => errors.push(`${during}: ${e.message}`),
  });
  return { actions, posted, cancelled, errors };
}

const repeat = { every: "day" as const, timeZone: "UTC" };
const done = (key: string, id: string): NewAction => ({ id, kind: "done", key, payload: {} });

test("done on an agent item closes the local copy, waits for its bridge, and undoes by cancelling", async () => {
  const it = item("home/garden/ga-1");
  const hub = fakeHub([it]);
  let drawn = false;
  const h = await hub.actions.perform([done(it.key, "d1")], {
    onPatched: () => {
      drawn = true;
      assert.equal(it.status, "closed");
    },
  });
  assert.ok(drawn);
  assert.equal(it.closeReason, "Done in Meat Mode");
  assert.equal(it.closedAt, now.toISOString());
  assert.equal(it.pending, true);
  assert.equal(h.posted.length, 1);
  assert.equal(movedOn(h.posted[0]), false);

  await hub.actions.undo(h);
  assert.deepEqual(hub.cancelled, ["d1"]);
  assert.deepEqual(hub.posted.map((a) => a.kind), ["done"]);
});

test("done on a personal item is applied at once, so its Undo falls back to a reopen on 409", async () => {
  const it = item("hub/personal/pe-1");
  const hub = fakeHub([it]);
  const h = await hub.actions.perform([done(it.key, "d1")]);
  assert.equal(it.status, "closed");
  assert.equal(it.pending, false);
  await hub.actions.undo(h);
  assert.deepEqual(hub.cancelled, []);
  assert.deepEqual(hub.posted.map((a) => [a.kind, a.key]), [
    ["done", it.key],
    ["reopen", it.key],
  ]);
});

test("done on a repeating personal item leaves it open and undoes with the edit the reply's was holds", async () => {
  const it = item("hub/personal/pe-2", { dueAt: due, repeat });
  const hub = fakeHub([it]);
  const h = await hub.actions.perform([done(it.key, "d1")]);
  assert.equal(it.status, "open");
  assert.ok(movedOn(h.posted[0]));

  // The hub's next state moves it on; Undo puts the old date back locally before it posts.
  it.dueAt = nextDue;
  let seen = "";
  await hub.actions.undo(h, { onPatched: () => (seen = it.dueAt ?? "") });
  assert.equal(seen, due);
  assert.deepEqual(hub.cancelled, []);
  const u = hub.posted[1];
  assert.equal(u.kind, "edit");
  assert.deepEqual(u.payload.set, { due });
});

test("a mixed batch undoes each item its own way, in one call", async () => {
  const plain = item("hub/personal/pe-1");
  const rep = item("hub/personal/pe-2", { dueAt: due, repeat });
  const agent = item("home/garden/ga-1");
  const hub = fakeHub([plain, rep, agent]);
  const h = await hub.actions.perform([done(plain.key, "d1"), done(rep.key, "d2"), done(agent.key, "d3")]);
  assert.deepEqual([plain.status, rep.status, agent.status], ["closed", "open", "closed"]);
  assert.equal(h.posted.length, 3);

  await hub.actions.undo(h);
  assert.deepEqual(hub.cancelled, ["d3"]);
  assert.deepEqual(hub.posted.slice(3).map((a) => [a.kind, a.key]), [
    ["reopen", plain.key],
    ["edit", rep.key],
  ]);
});

test("a failed post is reported and left out of the handle", async () => {
  const a = item("hub/personal/pe-1");
  const b = item("hub/personal/pe-2");
  const hub = fakeHub([a, b], { failPost: (x) => x.key === a.key });
  const h = await hub.actions.perform([done(a.key, "d1"), done(b.key, "d2")]);
  assert.deepEqual(hub.errors, ["save: hub down"]);
  assert.deepEqual(h.posted.map((p) => p.sent.key), [b.key]);
});

test("a cancel that fails for another reason stops the Undo and says so", async () => {
  const a = item("home/garden/ga-1");
  const b = item("home/garden/ga-2");
  const hub = fakeHub([a, b], { failCancel: true });
  const h = await hub.actions.perform([done(a.key, "d1"), done(b.key, "d2")]);
  await hub.actions.undo(h);
  assert.deepEqual(hub.errors, ["undo: hub down"]);
  assert.equal(hub.posted.length, 2);
});

test("snooze undoes back to the time it had, and dismiss and reopen undo each other", async () => {
  const later = new Date(2026, 9, 15, 9, 0).toISOString();
  const s = item("hub/personal/pe-1");
  const gone = item("hub/personal/pe-2", { status: "closed", closeReason: "Dismissed", closedAt: due });
  const hub = fakeHub([s, gone]);
  const h = await hub.actions.perform([
    { id: "s1", kind: "snooze", key: s.key, payload: { until: later, previous: null } },
    { id: "r1", kind: "reopen", key: gone.key, payload: {} },
  ]);
  assert.equal(s.deferUntil, later);
  assert.deepEqual([gone.status, gone.closedAt, gone.closeReason], ["open", null, undefined]);
  await hub.actions.undo(h);
  assert.deepEqual(
    hub.posted.slice(2).map((a) => [a.kind, a.payload]),
    [
      ["snooze", { until: null, previous: later }],
      ["dismiss", {}],
    ],
  );
});

test("an action with no local change, such as a note, leaves the item alone and has no Undo", async () => {
  const it = item("home/garden/ga-1", { pending: false });
  const hub = fakeHub([it]);
  const h = await hub.actions.perform([{ id: "n1", kind: "note", key: it.key, payload: { text: "Water the plants first" } }]);
  assert.equal(it.pending, false);
  await hub.actions.undo(h);
  assert.deepEqual(hub.cancelled, []);
  assert.equal(hub.posted.length, 1);
});
