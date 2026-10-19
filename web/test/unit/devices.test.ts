import { test } from "node:test";
import assert from "node:assert/strict";
import { browserName } from "../../src/device.ts";
import { deviceStatus, kindLabel, revokeEffect } from "../../src/settings/devices.ts";
import { appStateMessage } from "../../src/push.ts";
import { SNOOZE_MS, actionBody, failureText, isPostedAction } from "../../src/sw-actions.ts";
import type { Device } from "../../src/types.ts";

test("the browser name is generic: browser and system, no versions", () => {
  assert.equal(browserName("Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"), "Firefox on Linux");
  assert.equal(browserName("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.0.0"), "Edge on Windows");
  assert.equal(browserName("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"), "Safari on macOS");
  assert.equal(browserName("Mozilla/5.0 (Linux; Android 14; Phone) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36"), "Chrome on Android");
  assert.equal(browserName("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"), "Safari on iOS");
  assert.equal(browserName(""), "Browser");
});

const base: Device = { id: "a".repeat(32), name: "Test phone", kind: "app", confirmed: true, createdAt: "2026-10-10T10:00:00Z", lastSeen: "2026-10-14T09:00:00Z", lastPush: null };
const now = new Date("2026-10-14T10:00:00Z");

test("a device line says when it last got a push and was seen", () => {
  assert.equal(kindLabel("app"), "Android app");
  assert.equal(kindLabel("web"), "Browser");
  assert.equal(deviceStatus(base, now), "No push yet · Last seen 1h ago");
  assert.equal(deviceStatus({ ...base, lastPush: "2026-10-14T09:30:00Z" }, now), "Last push 30m ago · Last seen 1h ago");
  assert.equal(deviceStatus({ ...base, confirmed: false }, now), "Waiting for confirmation");
});

test("revoking says what happens for each kind", () => {
  assert.match(revokeEffect("app"), /shows reminders as off/);
  assert.match(revokeEffect("web"), /opens Meat Mode again/);
});

test("the app's blocked and unavailable states have plain messages", () => {
  assert.match(appStateMessage("blocked"), /Android settings/);
  assert.match(appStateMessage("unavailable"), /UnifiedPush/);
  for (const s of ["off", "waiting", "on", undefined] as const) assert.equal(appStateMessage(s), "");
});

test("notification buttons post retry, done and snooze", () => {
  const at = new Date("2026-10-14T10:00:00Z");
  assert.deepEqual(actionBody("retry", "home/x/1", "i1", at), { id: "i1", kind: "ask", key: "home/x/1", payload: {} });
  assert.deepEqual(actionBody("done", "home/x/1", "i2", at), { id: "i2", kind: "done", key: "home/x/1", payload: {} });
  assert.deepEqual(actionBody("snooze", "home/x/1", "i3", at), {
    id: "i3",
    kind: "snooze",
    key: "home/x/1",
    payload: { until: "2026-10-14T11:00:00.000Z", previous: null },
  });
  assert.equal(SNOOZE_MS, 3600_000);
  assert.ok(isPostedAction("done") && isPostedAction("snooze") && isPostedAction("retry"));
  assert.ok(!isPostedAction("reply") && !isPostedAction("open") && !isPostedAction(""));
});

test("a refused button action says the hub's reason in plain words", () => {
  assert.equal(failureText(409, '{"error":"finish or remove its open sub-task first"}'), "Finish or remove its open sub-task first.");
  assert.equal(failureText(409, '{"error":"finish or remove its 2 open sub-tasks first"}'), "Finish or remove its 2 open sub-tasks first.");
  assert.equal(failureText(500, '{"error":"Try again."}'), "Try again.");
  assert.equal(failureText(502, "<html>bad gateway</html>"), "The hub could not do that.");
  assert.equal(failureText(0, ""), "Could not reach the hub.");
});
