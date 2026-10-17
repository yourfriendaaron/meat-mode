import { test } from "node:test";
import assert from "node:assert/strict";
import { evaluate, type Probe } from "../../src/connection.ts";

const HUB_TIME = "2026-03-02T09:00:00Z";

function probe(over: Partial<Probe> = {}, status: Partial<Probe["status"]> = {}): Probe {
  return {
    status: { login: "app", hostMatches: true, publicUrl: "https://meat-mode.example.com", pushKeys: true, ntfy: false, time: HUB_TIME, ...status },
    origin: "https://meat-mode.example.com",
    host: "meat-mode.example.com",
    secure: true,
    live: "ok",
    deviceMs: Date.parse(HUB_TIME),
    ...over,
  };
}

const byId = (p: Probe) => Object.fromEntries(evaluate(p).map((c) => [c.id, c]));

test("a healthy connection passes every check, and ntfy is only optional", () => {
  const c = byId(probe());
  for (const id of ["address", "secure", "live", "clock", "push"]) assert.equal(c[id].state, "pass", id);
  assert.equal(c.ntfy.state, "info");
  assert.equal(byId(probe({}, { ntfy: true })).ntfy.state, "pass");
});

test("an address that is not the public one fails, by Host or by origin", () => {
  const wrongHost = byId(probe({}, { hostMatches: false })).address;
  assert.equal(wrongHost.state, "fail");
  assert.match(wrongHost.fix!, /Open the app at https:\/\/meat-mode\.example\.com/);
  assert.match(wrongHost.fix!, /MEAT_MODE_PUBLIC_URL/);
  assert.equal(byId(probe({ origin: "http://meat-mode.example.com" })).address.state, "fail");
});

test("an insecure page fails with the https fix", () => {
  const c = byId(probe({ secure: false })).secure;
  assert.equal(c.state, "fail");
  assert.match(c.fix!, /https:\/\//);
});

test("live updates that do not arrive point at proxy buffering", () => {
  const c = byId(probe({ live: "timeout" })).live;
  assert.equal(c.state, "fail");
  assert.match(c.detail, /within 5 seconds/);
  assert.match(c.fix!, /proxy_buffering off/);
  assert.match(byId(probe({ live: "unsupported" })).live.detail, /cannot receive/);
});

test("a clock more than a minute off fails, in either direction", () => {
  const t = Date.parse(HUB_TIME);
  assert.equal(byId(probe({ deviceMs: t + 60_000 })).clock.state, "pass");
  const ahead = byId(probe({ deviceMs: t + 5 * 60_000 })).clock;
  assert.equal(ahead.state, "fail");
  assert.match(ahead.detail, /about 5 minutes ahead of the hub/);
  assert.match(byId(probe({ deviceMs: t - 90_000 })).clock.detail, /behind the hub/);
  assert.match(ahead.fix!, /automatic date and time/);
});

test("missing push keys fail with the fix", () => {
  const c = byId(probe({}, { pushKeys: false })).push;
  assert.equal(c.state, "fail");
  assert.match(c.fix!, /data folder/);
});
