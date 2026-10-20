import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  addMachineError,
  agentInstructions,
  agentMachines,
  agentsOnCommand,
  durationText,
  harnessLinesHTML,
  harnessText,
  installCommand,
  instructionFile,
  editableLimits,
  limitError,
  limitsBoxHTML,
  limitsChange,
  limitsText,
  machineReposHTML,
  pickMachine,
  PLATFORMS,
} from "../../src/agents.ts";
import { ApiError, retryAfterSeconds, waitText } from "../../src/api.ts";
import type { AgentLimits, Bridge, Repo, RunLimits } from "../../src/types.ts";

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
  const text = agentInstructions("Sam", { name: "desk" });
  assert.match(text, /^## Tasks for Sam\n/);
  assert.match(text, /paired with it as "desk"/);
  assert.match(text, /bd create "[^"]+" -l human/);
  assert.match(text, /meat-mode add "[^"]+" --project \. /);
  assert.match(text, /meat-mode add "Renew the library card"/);
  assert.match(text, /meat-mode edit mm-12 --due/);
  assert.match(text, /meat-mode done mm-12/);
  assert.match(text, /meat-mode reopen mm-12/);
  assert.match(text, /--clear-due/);
  assert.match(text, /--every makes it repeat/);
  assert.match(text, /--no-repeat/);
  assert.match(text, /--until 2026-11-26 ends the repeat/);
  assert.match(text, /--no-until/);
  assert.doesNotMatch(text, /https?:\/\//);
  assert.match(agentInstructions(" ", { name: "desk" }), /^## Tasks for the user\n/);
});

const raises = {
  beads: { name: "beads", title: "beads", version: "0.40.0", raise: 'bd create "Approve the footer text" -l human' },
  backlog: { name: "backlog", title: "Backlog.md", raise: 'backlog task create "Approve the footer text" --labels human' },
  ticket: { name: "ticket", title: "ticket", raise: 'tk create "Approve the footer text" --tags human' },
};

test("a machine that has not said which trackers it runs gets the beads wording and the project line", () => {
  for (const trackers of [undefined, [], [{ name: "beads", version: "0.40.0" }]]) {
    const text = agentInstructions("Sam", { name: "desk", trackers });
    assert.match(text, /raise a bead labelled human/);
    assert.match(text, /check the bead before asking again/);
    assert.match(text, /In a repo with no tracker, .*\n\n    meat-mode add "[^"]+" --project \. /);
    assert.doesNotMatch(text, /In a repo that uses/);
  }
});

test("one tracker gets its own raise command", () => {
  const text = agentInstructions("Sam", { name: "desk", trackers: [raises.ticket] });
  assert.match(text, /In a repo that uses ticket:\n\n    tk create "Approve the footer text" --tags human\n/);
  assert.doesNotMatch(text, /bd create/);
  assert.match(text, /comes back to the task: done closes it and a note becomes a comment, so check the task before asking again/);
  assert.match(text, /--project \./);
  assert.match(text, /personal to-do list instead/);
});

test("several trackers get one raise line each, by the name the app shows", () => {
  const text = agentInstructions("Sam", { name: "desk", trackers: [raises.beads, raises.backlog, raises.ticket] });
  const uses = [...text.matchAll(/In a repo that uses ([^:]+):\n\n    (.+)/g)].map((m) => [m[1], m[2]]);
  assert.deepEqual(uses, [
    ["beads", raises.beads.raise],
    ["Backlog.md", raises.backlog.raise],
    ["ticket", raises.ticket.raise],
  ]);
  assert.match(text, /In a repo with no tracker/);
});

test("an older machine that names its trackers without their commands gets each tracker's own command", () => {
  const text = agentInstructions("Sam", { name: "desk", trackers: [{ name: "ticket" }, { name: "backlog", version: "1.54.0" }] });
  assert.match(text, /In a repo that uses ticket:\n\n    tk create "Approve the footer text" --tags human /);
  assert.match(text, /In a repo that uses Backlog\.md:\n\n    backlog task create "Approve the footer text" --labels human /);
  assert.doesNotMatch(text, /bd create|raise a bead/);
  assert.match(text, /check the task before asking again/);
});

test("a tracker the app does not know is named without a command", () => {
  const text = agentInstructions("Sam", { name: "desk", trackers: [{ name: "plans", title: "Plans" }] });
  assert.match(text, /In a repo that uses Plans, create the task with that tracker's own command and label it human\./);
  assert.doesNotMatch(text, /bd create|raise a bead/);
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

test("a machine's harness shows with its version and its limits in plain words", () => {
  const codex: Bridge = {
    name: "desk",
    createdAt: "2026-03-01T09:00:00Z",
    lastSeen: "2026-03-01T09:05:00Z",
    harness: { name: "codex", title: "Codex", version: "0.50.0", instructionFile: "AGENTS.md", budget: "none", timeoutSeconds: 1800 },
  };
  assert.equal(harnessText(codex), "Codex 0.50.0");
  assert.equal(limitsText(codex), "Time limit: 30 minutes a run · No spending cap: a run stops at its time limit · Notes wait until the agent finishes its turn");
  assert.deepEqual(instructionFile(codex), { file: "AGENTS.md", harness: "Codex" });

  const claude: Bridge = {
    ...codex,
    harness: { name: "claude", title: "Claude Code", instructionFile: "CLAUDE.md", budget: "native", budgetUsd: 2, timeoutSeconds: 5400, steer: true },
  };
  assert.equal(limitsText(claude), "Time limit: 1 hour 30 minutes a run · Stops at $2 a run");
  assert.equal(limitsText({ harness: { name: "pi", budget: "metered", budgetUsd: 0.5, steer: true } }), "Stops at $0.50 a run");
  assert.match(harnessLinesHTML(codex), /Agents run in Codex 0\.50\.0.*class="muted small m-limits">Time limit/);
});

test("a machine that sends no harness runs Claude Code, with no limits line and the old instruction wording", () => {
  const old: Bridge = { name: "desk", createdAt: "2026-03-01T09:00:00Z", lastSeen: "2026-03-01T09:05:00Z" };
  assert.equal(harnessText(old), "Claude Code");
  assert.equal(limitsText(old), "");
  assert.equal(instructionFile(old), null);
  assert.equal(harnessLinesHTML(old), '<div class="muted small m-harness">Agents run in Claude Code</div>');
  // A machine never seen has not said which harness it runs.
  assert.equal(harnessLinesHTML({ ...old, lastSeen: null }), "");
  // A harness that names no file keeps the old wording too.
  assert.equal(instructionFile({ harness: { name: "pi" } }), null);
});

test("time limits read in seconds, minutes or hours", () => {
  assert.equal(durationText(45), "45 seconds");
  assert.equal(durationText(60), "1 minute");
  assert.equal(durationText(3600), "1 hour");
  assert.equal(durationText(7260), "2 hours 1 minute");
});

const MAX: AgentLimits = { budgetUsd: 20, timeoutSeconds: 14400, runsPerTask: 50, concurrent: 5 };
const LIMITS: RunLimits = {
  budgetUsd: 5,
  timeoutSeconds: 1800,
  runsPerTask: 10,
  concurrent: 2,
  defaults: { budgetUsd: 2, timeoutSeconds: 1800, runsPerTask: 10, concurrent: 2 },
  max: MAX,
  set: ["budgetUsd"],
};

test("a limit is refused below its floor and above the machine's ceiling, naming the variable that raises it", () => {
  assert.equal(limitError("budgetUsd", "20", MAX, "desk"), "");
  assert.equal(limitError("budgetUsd", "0", MAX, "desk"), "The spending cap must be more than $0.");
  assert.equal(limitError("budgetUsd", "", MAX, "desk"), "The spending cap must be more than $0.");
  assert.equal(
    limitError("budgetUsd", "20.5", MAX, "desk"),
    "The spending cap can be at most $20 a run on desk. To allow more, set MEAT_MODE_AGENT_MAX_BUDGET_USD on desk and restart its bridge.",
  );
  assert.equal(limitError("timeoutSeconds", "240", MAX, "desk"), "");
  assert.equal(limitError("timeoutSeconds", "1.5", MAX, "desk"), "The time limit must be a whole number of minutes, at least 1.");
  assert.match(limitError("timeoutSeconds", "241", MAX, "desk"), /^The time limit can be at most 4 hours a run on desk\. .*MEAT_MODE_AGENT_MAX_TIMEOUT/);
  assert.equal(limitError("runsPerTask", "0", MAX, "desk"), "Runs per task must be a whole number of 1 or more.");
  assert.match(limitError("runsPerTask", "51", MAX, "desk"), /^Runs per task can be at most 50 on desk\. .*MEAT_MODE_AGENT_MAX_RUNS_PER_TASK/);
  assert.match(limitError("concurrent", "6", MAX, "desk"), /MEAT_MODE_AGENT_MAX_CONCURRENT on desk/);
});

test("the limits form sends only what changed, the time limit in seconds", () => {
  const shown = { budgetUsd: "5", timeoutSeconds: "30", runsPerTask: "10", concurrent: "2" };
  assert.deepEqual(limitsChange({ ...shown }, shown, MAX, "desk"), { change: {}, error: "" });
  assert.deepEqual(limitsChange({ ...shown, timeoutSeconds: "90", concurrent: " 3 " }, shown, MAX, "desk"), {
    change: { timeoutSeconds: 5400, concurrent: 3 },
    error: "",
  });
  assert.match(limitsChange({ ...shown, budgetUsd: "50" }, shown, MAX, "desk").error, /at most \$20/);
});

test("the limits box offers the form, a reset for a limit the app set, and none on an older bridge", () => {
  const b: Bridge = {
    name: "desk",
    createdAt: "2026-03-01T09:00:00Z",
    lastSeen: "2026-03-01T09:05:00Z",
    harness: { name: "claude", title: "Claude Code", budget: "native", budgetUsd: 5, timeoutSeconds: 1800, limits: LIMITS },
  };
  assert.match(limitsBoxHTML(b, { open: false }), /data-act="limits-edit"/);
  assert.equal(limitsText(b), "Time limit: 30 minutes a run · Stops at $5 a run · 10 runs per task · 2 at once · Notes wait until the agent finishes its turn");
  const open = limitsBoxHTML(b, { open: true });
  assert.equal(open.match(/data-act="limit-reset"/g)?.length, 1);
  assert.match(open, /Up to \$20 · Machine default \$2/);
  assert.match(open, /name="timeoutSeconds"[^>]*value="30"/);
  // A harness with no cost has no cap to set.
  const codex = { ...b, harness: { ...b.harness!, budget: "none" as const } };
  assert.deepEqual(editableLimits(codex), ["timeoutSeconds", "runsPerTask", "concurrent"]);
  assert.doesNotMatch(limitsBoxHTML(codex, { open: true }), /name="budgetUsd"/);
  // An older bridge sends no limits: they show, but cannot change here.
  const old = { ...b, harness: { ...b.harness!, limits: undefined } };
  assert.match(limitsBoxHTML(old, { open: true }), /Update the bridge on desk/);
  assert.equal(limitsBoxHTML({ ...b, lastSeen: null }, { open: false }), "");
  assert.match(limitsBoxHTML({ ...b, limitsPending: true }, { open: false }), /role="status">Waiting for desk/);
});
