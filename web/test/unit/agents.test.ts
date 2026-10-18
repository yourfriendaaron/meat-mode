import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { addMachineError, agentInstructions, agentMachines, agentsOnCommand, installCommand, machineReposHTML, pickMachine, PLATFORMS, waitText } from "../../src/agents.ts";
import { ApiError, retryAfterSeconds } from "../../src/api.ts";
import type { Bridge, Repo } from "../../src/types.ts";

test("each install download is a binary the release workflow builds", () => {
  const workflow = readFileSync(new URL("../../../.github/workflows/release.yml", import.meta.url), "utf8");
  const targets = workflow.match(/for t in ([^;]+);/)![1].trim().split(/\s+/);
  const downloads = PLATFORMS.filter((p) => p.id !== "go").map((p) => p.id.replace("-", "/"));
  assert.deepEqual([...downloads].sort(), [...targets].sort());
  assert.match(workflow, /-o "dist\/meat-mode-\$\{t%\/\*\}-\$\{t#\*\/\}"/);
  assert.match(installCommand("linux-arm64"), /\/releases\/latest\/download\/meat-mode-linux-arm64 /);
  assert.equal(installCommand("go"), "go install github.com/yourfriendaaron/meat-mode/cmd/meat-mode@latest");
});

test("the instructions name the user and the machine, and say how to raise and add tasks", () => {
  const text = agentInstructions("Sam", "desk");
  assert.match(text, /^## Tasks for Sam\n/);
  assert.match(text, /paired with it as "desk"/);
  assert.match(text, /bd create "[^"]+" -l human/);
  assert.match(text, /meat-mode add "[^"]+"/);
  assert.match(text, /meat-mode edit mm-12 --due/);
  assert.match(text, /meat-mode done mm-12/);
  assert.match(text, /meat-mode reopen mm-12/);
  assert.match(text, /--clear-due/);
  assert.match(text, /--every makes it repeat/);
  assert.match(text, /--no-repeat/);
  assert.match(text, /--until 2026-11-26 ends the repeat/);
  assert.match(text, /--no-until/);
  assert.doesNotMatch(text, /https?:\/\//);
  assert.match(agentInstructions(" ", "desk"), /^## Tasks for the user\n/);
});

const b = (name: string, createdAt: string): Bridge => ({ name, createdAt });

test("the hub's own entry is not an agent machine, and the newest machine is picked unless one was chosen", () => {
  const machines = agentMachines([b("hub", "2026-10-01T00:00:00Z"), b("laptop", "2026-10-02T00:00:00Z"), b("desk", "2026-10-03T00:00:00Z")]);
  assert.deepEqual(
    machines.map((m) => m.name),
    ["laptop", "desk"],
  );
  assert.equal(pickMachine(machines, ""), "desk");
  assert.equal(pickMachine(machines, "laptop"), "laptop");
  assert.equal(pickMachine(machines, "gone"), "desk");
  assert.equal(pickMachine([], ""), "");
});

test("agents switch on at the machine, by the repo's name, quoted for the shell when it needs it", () => {
  assert.equal(agentsOnCommand("projects/garden"), "meat-mode bridge agents on projects/garden");
  assert.equal(agentsOnCommand("Sam's notes"), "meat-mode bridge agents on 'Sam'\\''s notes'");
});

test("a repo with agents off shows the command and no switch; one with agents on shows only a switch that turns them off", () => {
  const repo = (name: string, over: Partial<Repo>): Repo => ({ bridge: "desk", name, personal: false, optedIn: true, ...over });
  const html = machineReposHTML([repo("garden", { agentsAllowed: false }), repo("website", { agentsAllowed: true }), repo("notes", { optedIn: false })], "desk");
  const rows = html.split('<li class="m-repo">').slice(1);
  const [garden, notes, website] = rows;
  assert.match(garden, /meat-mode bridge agents on garden/);
  assert.match(garden, /data-act="agents-on-copy"/);
  assert.doesNotMatch(garden, /data-act="agents-switch"/);
  assert.match(website, /aria-checked="true" aria-label="Agents can work in [^"]*" data-act="agents-switch"/);
  assert.doesNotMatch(website, /bridge agents on/);
  // A repo that does not sync says agents are off, without the command.
  assert.match(notes, /Agents are off here/);
  assert.doesNotMatch(notes, /bridge agents on/);
});

test("adding a machine asks for the password again on the hub's word, and says why it failed", () => {
  assert.deepEqual(addMachineError(new ApiError(403, "enter your password to add a machine", true), false), { error: "", reauth: true });
  assert.deepEqual(addMachineError(new ApiError(403, "wrong password", true), true), { error: "Wrong password.", reauth: true });
  assert.deepEqual(addMachineError(new ApiError(429, "too many failed logins", false, 60), true), {
    error: "Too many tries. Wait a minute and try again.",
    reauth: true,
  });
  assert.equal(addMachineError(new ApiError(429, "x", false, 150), true).error, "Too many tries. Wait 3 minutes and try again.");
  assert.deepEqual(addMachineError(new ApiError(409, "a machine with that name exists"), false), { error: "a machine with that name exists", reauth: false });
  assert.equal(waitText(0), "a minute");
  assert.equal(waitText(20), "20 seconds");
  assert.equal(retryAfterSeconds("30"), 30);
  assert.equal(retryAfterSeconds(null), 0);
  assert.equal(retryAfterSeconds(new Date(90_000).toUTCString(), 0), 90);
});
