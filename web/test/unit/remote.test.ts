import { test } from "node:test";
import assert from "node:assert/strict";
import { ApiError, errorText } from "../../src/api.ts";
import { remote } from "../../src/remote.ts";

/** A load the test answers by hand, counting the calls. */
function manual<T>() {
  const calls: { ok: (v: T) => void; fail: (e: unknown) => void }[] = [];
  const load = () => new Promise<T>((ok, fail) => calls.push({ ok, fail }));
  return { load, calls };
}

test("get loads once, draws when the load ends, and keeps the value", async () => {
  const m = manual<string[]>();
  const r = remote(m.load);
  let drawn = 0;
  assert.equal(r.get(() => drawn++), null);
  assert.equal(r.loading(), true);
  assert.equal(r.get(() => drawn++), null, "a load under way is not started twice");
  assert.equal(m.calls.length, 1);
  m.calls[0].ok(["Water the plants"]);
  await new Promise((res) => setTimeout(res, 0));
  assert.equal(drawn, 1);
  assert.deepEqual(r.get(), ["Water the plants"]);
  assert.equal(m.calls.length, 1, "a loaded value is not fetched again without maxAge");
  assert.equal(r.loading(), false);
});

test("a failed load says why in errorText's words, keeps the last value, and waits for reload", async () => {
  const m = manual<number>();
  const r = remote(m.load);
  r.get();
  m.calls[0].fail(new ApiError(429, "too many", false, 120));
  await new Promise((res) => setTimeout(res, 0));
  assert.equal(r.error(), "Too many tries. Wait 2 minutes and try again.");
  assert.equal(r.get(), null);
  assert.equal(m.calls.length, 1, "an error stays up until reload");
  const again = r.reload();
  assert.equal(r.error(), "", "a load under way clears the error");
  m.calls[1].ok(5);
  await again;
  assert.equal(r.get(), 5);
  const third = r.reload();
  m.calls[2].fail(new Error("offline"));
  await third;
  assert.equal(r.peek(), 5, "a failed reload keeps the last value");
  assert.equal(r.error(), "offline");
});

test("maxAge makes a value or an error stale", async () => {
  const m = manual<number>();
  const r = remote(m.load, { maxAge: 50 });
  r.get();
  m.calls[0].fail(new Error("offline"));
  await new Promise((res) => setTimeout(res, 0));
  assert.equal(r.stale(), false, "a fresh error counts as recent");
  await new Promise((res) => setTimeout(res, 60));
  assert.equal(r.stale(), true);
  r.get();
  m.calls[1].ok(1);
  await new Promise((res) => setTimeout(res, 0));
  assert.equal(r.stale(), false);
  await new Promise((res) => setTimeout(res, 60));
  r.get();
  assert.equal(m.calls.length, 3);
});

test("the latest load wins, and forget or set drops a load under way", async () => {
  const m = manual<string>();
  const r = remote(m.load);
  const first = r.reload();
  const second = r.reload();
  m.calls[1].ok("new");
  await second;
  m.calls[0].ok("old");
  await first;
  assert.equal(r.peek(), "new");

  r.reload();
  r.forget();
  assert.equal(r.loading(), false);
  m.calls[2].ok("forgotten");
  await new Promise((res) => setTimeout(res, 0));
  assert.equal(r.peek(), null);
  assert.equal(r.stale(), true);

  r.reload();
  r.set("from a change");
  m.calls[3].ok("from before the change");
  await new Promise((res) => setTimeout(res, 0));
  assert.equal(r.peek(), "from a change");
  assert.equal(r.loading(), false);
});

test("errorText words the hub's refusals the same way everywhere", () => {
  assert.equal(errorText(new ApiError(500, "database is locked")), "database is locked");
  assert.equal(errorText(new TypeError("Failed to fetch")), "Failed to fetch");
  assert.equal(errorText("plain"), "plain");
  assert.equal(errorText(new ApiError(429, "x")), "Too many tries. Wait a minute and try again.");
  assert.equal(errorText(new ApiError(429, "x", false, 1)), "Too many tries. Wait 1 second and try again.");
  assert.equal(errorText(new ApiError(429, "x", false, 61)), "Too many tries. Wait 2 minutes and try again.");
  assert.equal(errorText(new ApiError(429, "x", false, 30), { tooMany: "Too many wrong codes" }), "Too many wrong codes. Wait 30 seconds and try again.");
  assert.equal(errorText(new ApiError(401, "wrong password")), "wrong password");
  assert.equal(errorText(new ApiError(401, "wrong password"), { unauthorized: "Wrong password." }), "Wrong password.");
  assert.equal(errorText(new ApiError(403, "no"), { forbidden: "Not allowed." }), "Not allowed.");
  assert.equal(errorText(new ApiError(403, "enter your password", true), { reauth: "", forbidden: "Not allowed." }), "");
  assert.equal(errorText(new ApiError(403, "enter your password", true), { forbidden: "Not allowed." }), "Not allowed.");
  assert.equal(errorText(new ApiError(409, "taken"), { other: (m) => `Could not add: ${m}` }), "Could not add: taken");
  assert.equal(errorText(new ApiError(429, "x"), { other: (m) => `Could not add: ${m}` }), "Too many tries. Wait a minute and try again.");
});
