import { test } from "node:test";
import assert from "node:assert/strict";
import { STREAK_MS, STREAK_STEPS, nextStreak } from "../../src/sound.ts";

test("a done climbs a step when it follows the last one closely", () => {
  assert.equal(nextStreak(null, 1000), 0);
  assert.equal(nextStreak({ at: 1000, step: 0 }, 1000 + STREAK_MS - 1), 1);
  assert.equal(nextStreak({ at: 1000, step: 3 }, 2000), 4);
});

test("a pause starts the streak again", () => {
  assert.equal(nextStreak({ at: 1000, step: 4 }, 1000 + STREAK_MS + 1), 0);
});

test("the streak stops climbing at the top of the scale", () => {
  const top = STREAK_STEPS.length - 1;
  assert.equal(nextStreak({ at: 1000, step: top }, 1500), top);
});

test("every step is in the major pentatonic scale", () => {
  for (const s of STREAK_STEPS) assert.ok([0, 2, 4, 7, 9].includes(s % 12), `${s}`);
});
