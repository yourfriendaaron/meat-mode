import { test } from "node:test";
import assert from "node:assert/strict";
import { can, repoCan } from "../../src/model.ts";
import { agentsAllowed, anyAgentsAllowed, noAgentReason } from "../../src/runs.ts";
import { machineReposHTML, trackersText } from "../../src/agents.ts";
import type { Item, Repo } from "../../src/types.ts";

// A tracker that marks done, dismisses and adds, but has no snooze, notes,
// reopen, sub-tasks or agents.
const shed: Repo = { bridge: "desk", name: "shed", personal: false, optedIn: true, agentsAllowed: true, tracker: "ticket", actions: ["done", "dismiss", "add"] };
const garden: Repo = { bridge: "desk", name: "garden", personal: false, optedIn: true, agentsAllowed: true };
const personal: Repo = { bridge: "hub", name: "personal", personal: true, optedIn: true };
const repos = [shed, garden, personal];

function item(r: Repo): Item {
  return { key: `${r.bridge}/${r.name}/x-1`, bridge: r.bridge, repo: r.name, id: "x-1", title: "Oil the hinges", status: "open", priority: 2, createdAt: "2026-10-14T09:00:00Z", updatedAt: "2026-10-14T09:00:00Z", comments: [] };
}

test("a list takes the kinds its tracker lists, and every kind when it lists none", () => {
  for (const kind of ["done", "dismiss", "add"] as const) assert.ok(can(item(shed), kind, repos), kind);
  for (const kind of ["snooze", "note", "reopen", "subtask", "ask"] as const) assert.ok(!can(item(shed), kind, repos), kind);
  // An older machine and the personal list send no actions.
  for (const kind of ["done", "snooze", "note", "reopen", "subtask", "ask"] as const) {
    assert.ok(can(item(garden), kind, repos), kind);
    assert.ok(can(item(personal), kind, repos), kind);
  }
  // A list the state does not hold yet is not narrowed either.
  assert.ok(can({ bridge: "desk", repo: "attic" }, "snooze", repos));
  assert.ok(repoCan(undefined, "note"));
});

test("agents work only where the tracker can ask, on top of the agents switch", () => {
  assert.equal(agentsAllowed(item(shed), repos), false);
  assert.equal(agentsAllowed(item(garden), repos), true);
  assert.equal(anyAgentsAllowed([shed, personal]), false);
  assert.equal(noAgentReason(shed), "ticket has no agents.");
  assert.equal(noAgentReason({ ...shed, tracker: undefined }), "This list's tracker has no agents.");
  assert.equal(noAgentReason(garden), "");
});

test("a machine's repos name their tracker, and one without agents has no agents line", () => {
  const html = machineReposHTML([shed, garden], "desk");
  const [g, s] = html.split('<li class="m-repo">').slice(1);
  assert.match(s, /shed · ticket/);
  assert.doesNotMatch(s, /Agents/);
  assert.match(g, /Agents can work here/);
  assert.doesNotMatch(g, / · /);
});

test("a machine's trackers show with their versions, and an older machine's bd version", () => {
  assert.equal(trackersText({ name: "desk", createdAt: "", trackers: [{ name: "beads", version: "0.40.0" }, { name: "ticket" }] }), "beads 0.40.0 · ticket");
  assert.equal(trackersText({ name: "desk", createdAt: "", bdVersion: "0.39.1" }), "bd 0.39.1");
  assert.equal(trackersText({ name: "desk", createdAt: "" }), "");
});
