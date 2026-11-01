import { test } from "node:test";
import assert from "node:assert/strict";
import { listBadge, listColourClass, listInitial, listSlot } from "../../src/identity.ts";
import type { Repo } from "../../src/types.ts";

const HASH = "0123456789abcdef0123456789abcdef";

function repo(name: string, over: Partial<Repo> = {}): Repo {
  return { bridge: "home", name, personal: false, optedIn: true, ...over };
}

const personal: Repo = { bridge: "hub", name: "personal", personal: true, optedIn: true };

test("a list's slot is the hub's colour, taken mod 6", () => {
  const all = [personal, repo("garden", { colour: 4 }), repo("website", { colour: 9 }), repo("recipes", { colour: -1 }), repo("notes", { colour: 2.7 })];
  assert.deepEqual(
    all.slice(1).map((r) => listSlot(r, all)),
    [4, 3, 5, 2],
  );
});

test("without a colour, from an older hub, the slot follows the order the app lists them", () => {
  const all = [repo("website"), repo("garden"), personal, repo("hidden", { optedIn: false })];
  // The personal list first, then by name: personal 0, garden 1, website 2.
  assert.equal(listSlot(all[1], all), 1);
  assert.equal(listSlot(all[0], all), 2);
  // A repo the app does not list takes the first slot.
  assert.equal(listSlot(all[3], all), 0);
  // A colour that is no number is ignored.
  assert.equal(listSlot(repo("garden", { colour: Number.NaN }), all), 1);
});

test("the letter is the first character of the list's label, upper case", () => {
  assert.equal(listInitial({ name: "projects/garden" }), "G");
  assert.equal(listInitial({ name: "éclair" }), "É");
  // A character outside the basic plane stays whole.
  assert.equal(listInitial({ name: "\u{1F331}sprouts" }), "\u{1F331}");
});

test("a badge is the letter on the list's colour, or the icon on a neutral tile with the letter behind it", () => {
  const all = [personal, repo("garden", { colour: 3 }), repo("website", { colour: 1, icon: HASH })];
  assert.equal(listBadge(all[1], all), '<span class="rb rb-m lc3" aria-hidden="true">G</span>');
  assert.equal(
    listBadge(all[2], all, "l"),
    `<span class="rb rb-l rb-icon lc1" aria-hidden="true"><img src="/api/icons/${HASH}" alt="" loading="lazy" data-icon="${HASH}"><span class="rb-t">W</span></span>`,
  );
  // A hash that is not one the hub gives is never put in a URL.
  assert.equal(listBadge(repo("garden", { colour: 0, icon: "../state" }), all, "s"), '<span class="rb rb-s lc0" aria-hidden="true">G</span>');
  // A label is escaped.
  assert.match(listBadge(repo("<b>", { colour: 0 }), all), /&lt;/);
});

test("the personal list keeps its own mark", () => {
  const html = listBadge({ ...personal, colour: 4, icon: HASH }, [personal]);
  assert.match(html, /^<span class="rb rb-m rb-own" aria-hidden="true"><svg/);
  assert.doesNotMatch(html, /img/);
  assert.equal(listColourClass(personal, [personal]), "lc-own");
  assert.equal(listColourClass(repo("garden", { colour: 5 }), []), "lc5");
});
