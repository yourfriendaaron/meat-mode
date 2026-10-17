import { test } from "node:test";
import assert from "node:assert/strict";
import { loginErrorText, needsLogin } from "../../src/login.ts";

test("the login screen shows only when the hub has a login and this browser is not in", () => {
  assert.equal(needsLogin({ login: true, authenticated: false }), true);
  assert.equal(needsLogin({ login: true, authenticated: true }), false);
  assert.equal(needsLogin({ login: false, authenticated: true }), false);
});

test("a refused login says why in plain words", () => {
  assert.equal(loginErrorText(401, "wrong password"), "Wrong password.");
  assert.equal(loginErrorText(429, "too many failed logins"), "Too many tries. Wait a minute and try again.");
  assert.equal(loginErrorText(500, "database is locked"), "Could not log in: database is locked");
});
