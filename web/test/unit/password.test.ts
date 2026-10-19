import { test } from "node:test";
import assert from "node:assert/strict";
import { ApiError } from "../../src/api.ts";
import { changeErrorText, checkPassword, claimErrorText } from "../../src/password.ts";
import { findStep } from "../../src/wizard/registry.ts";
import type { AppContext } from "../../src/app-context.ts";
import type { SetupInfo } from "../../src/types.ts";
import "../../src/wizard/steps/index.ts";

test("a new password needs 8 characters, counted as the hub counts them, typed the same twice", () => {
  assert.equal(checkPassword("short", "short"), "The password must be at least 8 characters.");
  assert.equal(checkPassword("ééééééé", "ééééééé"), "The password must be at least 8 characters.");
  assert.equal(checkPassword("éééééééé", "éééééééé"), "");
  assert.equal(checkPassword("water the plants", "water the plant"), "The two passwords do not match.");
  assert.equal(checkPassword("water the plants", "water the plants"), "");
});

test("refused claims and changes say what to do in plain words", () => {
  assert.equal(claimErrorText(new ApiError(403, "x")), "That setup code is wrong, expired or already used. Restart the hub for a new one.");
  assert.equal(claimErrorText(new ApiError(429, "x", false, 600)), "Too many wrong codes. Wait 10 minutes and try again.");
  assert.equal(claimErrorText(new ApiError(409, "this hub already has a password")), "This hub already has a password.");
  assert.equal(changeErrorText(new ApiError(403, "wrong password")), "The current password is wrong.");
  assert.equal(changeErrorText(new ApiError(429, "x")), "Too many tries. Wait a minute and try again.");
  assert.equal(changeErrorText(new ApiError(400, "the password must be at least 8 characters")), "The password must be at least 8 characters.");
  assert.equal(changeErrorText(new TypeError("failed to fetch")), "Failed to fetch.");
});

test("the Secure step counts as set up with any password, and with none only never", () => {
  const step = findStep("secure")!;
  const ctx = (setup: Partial<SetupInfo> | null) =>
    ({ setup: setup && { status: "new", user: "alex", timeZone: "", password: "none", ...setup } }) as AppContext;
  assert.equal(step.part, "hub");
  assert.equal(step.settings, "s-password");
  assert.equal(step.done!(ctx(null)), false);
  assert.equal(step.done!(ctx({})), false);
  assert.equal(step.done!(ctx({ password: "app" })), true);
  assert.equal(step.done!(ctx({ password: "env" })), true);
});
