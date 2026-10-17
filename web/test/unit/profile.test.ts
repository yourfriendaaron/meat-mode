import { test } from "node:test";
import assert from "node:assert/strict";
import { profileChange, validZone } from "../../src/profile.ts";

const kept = { user: "alex", timeZone: "" };

test("only a changed field is sent", () => {
  assert.deepEqual(profileChange(kept, { name: "alex", timeZone: "" }), {});
  assert.deepEqual(profileChange(kept, { name: " Sam ", timeZone: "" }), { name: "Sam" });
  assert.deepEqual(profileChange(kept, { name: "alex", timeZone: "Europe/Paris" }), { timeZone: "Europe/Paris" });
  assert.deepEqual(profileChange({ user: "Sam", timeZone: "Europe/Paris" }, { name: "Sam", timeZone: "Europe/Paris" }), {});
});

test("clearing the name goes back to the default, and an empty zone changes nothing", () => {
  assert.deepEqual(profileChange({ user: "Sam", timeZone: "Europe/Paris" }, { name: "", timeZone: "" }), { name: "" });
});

test("a zone the browser does not know is refused with a reason", () => {
  const got = profileChange(kept, { name: "alex", timeZone: "Mars/Olympus" });
  assert.equal(typeof got, "string");
  assert.match(got as string, /Mars\/Olympus/);
  assert.equal(validZone("America/New_York"), true);
  assert.equal(validZone("UTC"), true);
  assert.equal(validZone("Local"), false);
});
