import { test } from "node:test";
import assert from "node:assert/strict";
import { firstRun, parseProgress, partsOf, type Progress } from "../../src/wizard/rules.ts";
import { findStep, stepOutcome, stepsFor, registerStep } from "../../src/wizard/registry.ts";
import type { AppContext } from "../../src/app-context.ts";
import "../../src/wizard/steps/index.ts";

const run = (mode: Progress["mode"]): Progress => ({ mode, step: "welcome", done: [], skipped: [] });

test("a new hub opens the wizard; a hub in use does not", () => {
  assert.equal(firstRun("new", "", false, null), "first");
  assert.equal(firstRun("new", "dismissed", true, null), "first");
  assert.equal(firstRun("in-use", "", true, null), null);
  assert.equal(firstRun("done", "done", false, null), null);
  assert.equal(firstRun("dismissed", "dismissed", false, null), null);
});

test("this device's part is offered while reminders are off and it was not done or dismissed here", () => {
  assert.equal(firstRun("in-use", "", false, null), "device");
  assert.equal(firstRun("done", "", false, null), "device");
  assert.equal(firstRun("done", "", true, null), null);
  assert.equal(firstRun("done", "dismissed", false, null), null);
  assert.equal(firstRun("done", "done", false, null), null);
  // Without the hub's answer nothing is offered.
  assert.equal(firstRun(null, "", false, null), null);
});

test("a run in progress resumes, unless a first run was finished elsewhere", () => {
  assert.equal(firstRun("in-use", "", true, run("first")), "resume");
  assert.equal(firstRun(null, "", true, run("first")), "resume");
  assert.equal(firstRun("done", "", true, run("all")), "resume");
  assert.equal(firstRun("dismissed", "", true, run("device")), "resume");
  assert.equal(firstRun("done", "", true, run("first")), null);
  assert.equal(firstRun("dismissed", "", false, run("first")), "device");
});

test("saved progress is read back only when it is a run", () => {
  const p = run("all");
  p.done.push("welcome");
  assert.deepEqual(parseProgress(JSON.stringify(p)), p);
  assert.equal(parseProgress(null), null);
  assert.equal(parseProgress("{"), null);
  assert.equal(parseProgress(JSON.stringify({ mode: "later", step: "x", done: [], skipped: [] })), null);
  assert.equal(parseProgress(JSON.stringify({ mode: "first", step: "x", done: [1], skipped: [] })), null);
});

test("each mode holds its parts", () => {
  assert.deepEqual(partsOf("first"), ["hub", "device"]);
  assert.deepEqual(partsOf("all"), ["hub", "device"]);
  assert.deepEqual(partsOf("device"), ["device"]);
});

const ctx = { state: null, session: null, setup: null, view: "setup" } as unknown as AppContext;
// Steps ask the browser what this device can do; here it is a tab that is not installed.
globalThis.matchMedia ??= ((media: string) => ({ media, matches: false })) as unknown as typeof matchMedia;

test("a run sorts steps by order, keeps its parts and leaves out steps that do not apply", () => {
  registerStep({ id: "test-device", title: "Device thing", part: "device", order: 55, render: () => "" });
  registerStep({ id: "test-hub", title: "Hub thing", part: "hub", order: 25, render: () => "" });
  registerStep({ id: "test-never", title: "Never", part: "hub", order: 26, applies: () => false, render: () => "" });
  // Only the steps registered here; the real steps come and go with the epic's slices.
  const ids = (mode: Progress["mode"]) =>
    stepsFor(mode, ctx)
      .map((s) => s.id)
      .filter((id) => id === "welcome" || id === "finish" || id.startsWith("test-"));
  assert.deepEqual(
    ids("first"),
    ["welcome", "test-hub", "test-device", "finish"],
  );
  assert.deepEqual(
    ids("device"),
    ["welcome", "test-device", "finish"],
  );
  assert.throws(() => registerStep({ id: "welcome", title: "Again", part: null, order: 1, render: () => "" }));
});

test("Finish lists a step as set up, skipped or not set up", () => {
  const p = run("first");
  p.done.push("test-hub");
  p.skipped.push("test-device");
  assert.equal(stepOutcome(findStep("test-hub")!, p, ctx), "done");
  assert.equal(stepOutcome(findStep("test-device")!, p, ctx), "skipped");
  assert.equal(stepOutcome({ ...findStep("test-device")!, done: () => true }, p, ctx), "done");
  assert.equal(stepOutcome(findStep("welcome")!, p, ctx), "open");
  const html = findStep("finish")!.render(ctx, { mode: "first", steps: stepsFor("first", ctx), progress: p });
  assert.match(html, /Set up<\/span><span class="wiz-sum-title">Hub thing/);
  assert.match(html, /Skipped<\/span><span class="wiz-sum-title">Device thing/);
});
