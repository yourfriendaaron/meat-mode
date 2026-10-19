import { test } from "node:test";
import assert from "node:assert/strict";
import { ApiError } from "../../src/api.ts";
import { gate, loginErrorText, needsLogin } from "../../src/login.ts";

test("the login screen shows only when the hub has a password and this browser is not in", () => {
  assert.equal(needsLogin({ login: true, authenticated: false }), true);
  assert.equal(needsLogin({ login: true, authenticated: true }), false);
  assert.equal(needsLogin({ login: false, authenticated: false }), false);
});

test("a hub with no password shows only the claim step; one with a password logs in first", () => {
  assert.equal(gate({ login: false, authenticated: false }), "claim");
  assert.equal(gate({ login: true, authenticated: false }), "login");
  assert.equal(gate({ login: true, authenticated: true }), null);
});

test("a refused login says why in plain words", () => {
  assert.equal(loginErrorText(new ApiError(401, "wrong password")), "Wrong password.");
  assert.equal(loginErrorText(new ApiError(429, "too many failed logins", false, 56)), "Too many tries. Wait 56 seconds and try again.");
  assert.equal(loginErrorText(new ApiError(500, "database is locked")), "Could not log in: database is locked");
  assert.equal(loginErrorText(new TypeError("Failed to fetch")), "Could not log in: Failed to fetch");
});
