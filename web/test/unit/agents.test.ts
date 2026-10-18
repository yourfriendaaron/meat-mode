import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { agentInstructions, agentMachines, installCommand, pickMachine, PLATFORMS } from "../../src/agents.ts";
import type { Bridge } from "../../src/types.ts";

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
