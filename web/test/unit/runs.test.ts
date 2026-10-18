import { test } from "node:test";
import assert from "node:assert/strict";
import { agentName, agentsAllowed, anyAgentsAllowed, askLabel, noAgentReason, queuedComments, REASON_TEXT, runEnded, runLine, runLineText, workingItems } from "../../src/runs.ts";
import type { Item, Repo, Run, RunReason } from "../../src/types.ts";

const repos: Repo[] = [
  { bridge: "hub", name: "personal", personal: true, optedIn: true },
  { bridge: "home", name: "garden", personal: false, optedIn: true, agentsAllowed: true },
  { bridge: "home", name: "website", personal: false, optedIn: true },
];

function item(repo: string, over: Partial<Item> = {}): Item {
  const bridge = repo === "personal" ? "hub" : "home";
  return { key: `${bridge}/${repo}/x-1`, bridge, repo, id: "x-1", title: "Water the plants", status: "open", priority: 2, createdAt: "", updatedAt: "", comments: [], ...over } as Item;
}

function run(over: Partial<Run>): Run {
  return { id: "r1", state: "working", agent: "planner", startedAt: "2026-10-14T09:00:00Z", count: 1, ...over };
}

test("each state's line names the agent and says the state in words", () => {
  assert.equal(runLineText(runLine(run({ activity: "Shortening the second line" }))), "planner · Shortening the second line");
  assert.equal(runLineText(runLine(run({}))), "planner · Starting");
  assert.equal(runLineText(runLine(run({ state: "waiting", activity: "Asked which wording" }))), "planner replied · Asked which wording");
  assert.equal(runLineText(runLine(run({ state: "waiting" }))), "planner replied");
  assert.equal(runLineText(runLine(run({ state: "done", activity: "Closed it" }))), "planner finished · Closed it");
  assert.equal(runLine(run({ state: "done" })).state, "done");
});

test("a stopped run gives its reason, and the detail only in full", () => {
  const r = run({ state: "stopped", reason: "usage-limit", detail: "The limit resets at 14:00." });
  assert.equal(runLineText(runLine(r)), "planner stopped · It hit its usage limit.");
  assert.equal(runLineText(runLine(r, true)), "planner stopped · It hit its usage limit. The limit resets at 14:00.");
  assert.equal(runLineText(runLine(run({ state: "stopped" }))), "planner stopped · It stopped before it finished.");
});

test("every reason has plain words", () => {
  const reasons: RunReason[] = ["stopped", "usage-limit", "timeout", "budget", "run-limit", "busy", "not-allowed", "no-reply", "failed", "crashed", "restarted", "offline"];
  for (const r of reasons) assert.match(REASON_TEXT[r], /^[A-Z][^.]+$/, r);
});

test("the note box's pink button follows the run", () => {
  assert.deepEqual(askLabel(item("garden", { createdBy: "planner" })), { label: "Save and ask agent", kind: "ask" });
  assert.deepEqual(askLabel(item("garden", { run: run({}) })), { label: "Send to planner", kind: "send" });
  assert.deepEqual(askLabel(item("garden", { run: run({ state: "waiting" }) })), { label: "Reply to planner", kind: "reply" });
  assert.deepEqual(askLabel(item("garden", { run: run({ state: "stopped", reason: "timeout" }) })), { label: "Save and ask agent", kind: "ask" });
  assert.deepEqual(askLabel(item("garden", { run: run({ state: "done" }) })), { label: "Save and ask agent", kind: "ask" });
});

test("the agent's name: the run's, else the bead's creator, else agent", () => {
  assert.equal(agentName(item("garden", { createdBy: "gardener", run: run({ agent: "planner" }) })), "planner");
  assert.equal(agentName(item("garden", { createdBy: "gardener" })), "gardener");
  assert.equal(agentName(item("garden")), "agent");
});

test("agents work only on an agent item in a repo that allows them", () => {
  assert.equal(agentsAllowed(item("garden"), repos), true);
  assert.equal(agentsAllowed(item("website"), repos), false);
  assert.equal(agentsAllowed(item("personal"), repos), false);
  assert.equal(anyAgentsAllowed(repos), true);
  assert.equal(anyAgentsAllowed(repos.filter((r) => r.name !== "garden")), false);
});

test("New task's switch says why a list takes no agent", () => {
  assert.equal(noAgentReason(repos[0]), "Personal to-dos have no agent.");
  assert.equal(noAgentReason(repos[1]), "");
  assert.equal(noAgentReason(repos[2]), "Agents are off for this list. Turn them on in Settings.");
  assert.equal(noAgentReason(undefined), "");
});

test("the queued notes are the newest of the user's own, only while the run works", () => {
  const comments = [
    { id: "a", author: "alex-app", text: "First", createdAt: "1" },
    { id: "b", author: "planner", text: "Reply", createdAt: "2" },
    { id: "c", author: "alex-app", text: "Second", createdAt: "3" },
    { id: "d", author: "alex-app", text: "Third", createdAt: "4" },
  ];
  assert.deepEqual([...queuedComments(item("garden", { comments, run: run({ queued: 2 }) }), "alex")], ["c", "d"]);
  assert.deepEqual([...queuedComments(item("garden", { comments, run: run({ queued: 0 }) }), "alex")], []);
  assert.deepEqual([...queuedComments(item("garden", { comments, run: run({ state: "waiting", queued: 2 }) }), "alex")], []);
});

test("a run ends when it leaves working", () => {
  assert.equal(runEnded("working", "waiting"), true);
  assert.equal(runEnded("working", "stopped"), true);
  assert.equal(runEnded("working", "working"), false);
  assert.equal(runEnded(undefined, "done"), false);
  assert.equal(runEnded("working", undefined), false);
  assert.deepEqual(workingItems([item("garden", { run: run({}) }), item("website")]).length, 1);
});
