// Agent machines, shared by Settings and the setup wizard's agent steps: the
// repo switches under each machine, the command that lets agents work in a
// repo, adding a machine, the commands that install and pair the bridge, and
// the text that tells a machine's agents how to raise tasks.

import { ApiError, errorText } from "./api.ts";
import { esc } from "./html.ts";
import { HUB_BRIDGE, repoCan, repoLabel } from "./model.ts";
import type { Bridge, Repo } from "./types.ts";

/** Where release binaries download from; the names match .github/workflows/release.yml. */
const RELEASES = "https://github.com/yourfriendaaron/meat-mode/releases/latest/download";
const MODULE = "github.com/yourfriendaaron/meat-mode/cmd/meat-mode";

/** The platforms a release has a binary for, as the release names them. */
export const PLATFORMS = [
  { id: "linux-amd64", label: "Linux, x86-64" },
  { id: "linux-arm64", label: "Linux, ARM64" },
  { id: "darwin-arm64", label: "macOS, Apple silicon" },
  { id: "darwin-amd64", label: "macOS, Intel" },
  { id: "go", label: "Any, with Go" },
] as const;

export type Platform = (typeof PLATFORMS)[number]["id"];

/** The agent machines paired with the hub, without the hub's own entry. */
export function agentMachines(bridges: Bridge[] | null | undefined): Bridge[] {
  return (bridges ?? []).filter((b) => b.name !== HUB_BRIDGE);
}

/** The command that installs the meat-mode program on a machine of that platform. */
export function installCommand(p: Platform): string {
  if (p === "go") return `go install ${MODULE}@latest`;
  return `mkdir -p ~/.local/bin && curl -fsSL -o ~/.local/bin/meat-mode ${RELEASES}/meat-mode-${p} && chmod +x ~/.local/bin/meat-mode`;
}

/** The command that pairs a machine, with the token shown once. */
export function pairCommand(origin: string, token: string): string {
  return `meat-mode bridge pair ${origin} ${token}`;
}

/** The command, run on the agent machine, that lets agents work in a repo (docs/api.md, Agents). */
export function agentsOnCommand(repo: string): string {
  const name = /^[A-Za-z0-9._/@+-]+$/.test(repo) ? repo : `'${repo.replace(/'/g, "'\\''")}'`;
  return `meat-mode bridge agents on ${name}`;
}

/**
 * Where agents are off: the command that switches them on at the machine,
 * with a Copy button whose click reaches main.ts as "agents-on-copy". The
 * app cannot switch agents on itself.
 */
export function agentsOnHTML(bridge: string, repo: string, id: string): string {
  return `<p class="muted small">To let agents work here, run this on ${esc(bridge)}:</p>
<div class="cmd-row"><code class="cmd" id="${esc(id)}">${esc(agentsOnCommand(repo))}</code><button class="btn btn-small" data-act="agents-on-copy" data-copy="${esc(id)}" aria-label="Copy the command for ${esc(repo)}">Copy</button></div>`;
}

/**
 * The repos a machine found, each with its sync switch and, under it,
 * whether agents may work there. Agents switch on only at the machine, so a
 * repo with agents off shows that command (for a repo that syncs) and one
 * with agents on shows a switch that turns them off. A click on a switch
 * reaches main.ts as "sync-switch" or "agents-switch". idPrefix keeps the
 * ids apart where Settings and the wizard are both in the page.
 */
export function machineReposHTML(repos: Repo[], bridge: string, idPrefix = "sync-"): string {
  const list = repos.filter((r) => r.bridge === bridge).sort((a, b) => a.name.localeCompare(b.name));
  if (!list.length) return '<p class="muted small m-repos">No repos found on this machine yet.</p>';
  return `<ul class="m-repos" aria-label="Repos on ${esc(bridge)}">${list
    .map((r) => {
      const control = `<button class="switch" role="switch" id="${esc(`${idPrefix}${bridge}/${r.name}`)}" aria-checked="${r.optedIn}" aria-label="Sync ${esc(repoLabel(r))} (${esc(r.name)})" data-act="sync-switch" data-bridge="${esc(bridge)}" data-repo="${esc(r.name)}"><span class="switch-knob" aria-hidden="true"></span></button>`;
      const note = r.pending ? `<div class="muted small" role="status">Waiting for ${esc(bridge)}</div>` : "";
      const agentsNote = r.agentsPending ? `<div class="muted small" role="status">Waiting for ${esc(bridge)}</div>` : "";
      let agents: string;
      if (!repoCan(r, "ask")) {
        // Its tracker runs no agents, so there is no switch to show.
        agents = "";
      } else if (r.agentsAllowed === true) {
        const agentsId = `${idPrefix}agents-${bridge}/${r.name}`;
        const off = `<button class="switch" role="switch" id="${esc(agentsId)}" aria-checked="true" aria-label="Agents can work in ${esc(repoLabel(r))} (${esc(r.name)})" data-act="agents-switch" data-bridge="${esc(bridge)}" data-repo="${esc(r.name)}"><span class="switch-knob" aria-hidden="true"></span></button>`;
        agents = `<div class="m-repo-agents"><div class="m-repo-name"><div class="small">Agents can work here</div>${agentsNote}</div>${off}</div>`;
      } else {
        const how = r.optedIn && !r.agentsPending ? agentsOnHTML(bridge, r.name, `${idPrefix}agents-cmd-${bridge}/${r.name}`) : "";
        agents = `<div class="m-repo-agents m-repo-agents-off"><div class="m-repo-name"><div class="small">Agents are off here</div>${agentsNote}${how}</div></div>`;
      }
      return `<li class="m-repo"><div class="m-repo-main"><div class="m-repo-name"><div>${esc(repoLabel(r))}</div><div class="muted small">${esc(r.tracker ? `${r.name} · ${r.tracker}` : r.name)}</div>${note}</div>${control}</div>${agents}</li>`;
    })
    .join("")}</ul>`;
}

/**
 * The trackers a machine runs, with their tools' versions, as "beads 0.40.0 ·
 * ticket". A machine from before trackers names only bd's version.
 */
export function trackersText(b: Bridge): string {
  if (b.trackers?.length) return b.trackers.map((t) => [t.title || t.name, t.version].filter(Boolean).join(" ")).join(" · ");
  return b.bdVersion ? `bd ${b.bdVersion}` : "";
}

/** The harness a machine's agents run in, by the name the app shows. A bridge that sends none predates harnesses and runs Claude Code. */
export function harnessTitle(b: Pick<Bridge, "harness">): string {
  return b.harness ? b.harness.title || b.harness.name : "Claude Code";
}

/** The harness with its version, as "Codex 0.50.0". */
export function harnessText(b: Pick<Bridge, "harness">): string {
  return [harnessTitle(b), b.harness?.version].filter(Boolean).join(" ");
}

/** A time limit in words: "45 seconds", "30 minutes", "1 hour 30 minutes". */
export function durationText(seconds: number): string {
  const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"}`;
  if (seconds < 60) return plural(seconds, "second");
  const mins = Math.round(seconds / 60);
  if (mins < 60) return plural(mins, "minute");
  const h = Math.floor(mins / 60);
  return mins % 60 ? `${plural(h, "hour")} ${plural(mins % 60, "minute")}` : plural(h, "hour");
}

/** A cap in dollars: "$2", "$2.50". */
function dollars(usd: number): string {
  return Number.isInteger(usd) ? `$${usd}` : `$${usd.toFixed(2)}`;
}

/**
 * The limits a machine's harness keeps on each run, in plain words joined by
 * " · ": the time limit, the spending cap or that it has none, and that notes
 * wait for the turn where they cannot reach a live run. Empty for a bridge
 * that sends no harness, whose limits the app does not know.
 */
export function limitsText(b: Pick<Bridge, "harness">): string {
  const h = b.harness;
  if (!h) return "";
  const out: string[] = [];
  if (h.timeoutSeconds) out.push(`Time limit: ${durationText(h.timeoutSeconds)} a run`);
  if (h.budget === "none") out.push("No spending cap: a run stops at its time limit");
  else if (h.budgetUsd) out.push(`Stops at ${dollars(h.budgetUsd)} a run`);
  if (!h.steer) out.push("Notes wait until the agent finishes its turn");
  return out.join(" · ");
}

/**
 * A machine's harness and its limits as two small lines, for Settings. Empty
 * for a machine never seen, which has not said which harness it runs.
 */
export function harnessLinesHTML(b: Bridge): string {
  if (!b.lastSeen) return "";
  const limits = limitsText(b);
  return `<div class="muted small m-harness">Agents run in ${esc(harnessText(b))}</div>${limits ? `<div class="muted small m-limits">${esc(limits)}</div>` : ""}`;
}

/** Where the harness is picked; the app cannot change it. */
export const HARNESS_PICK = "Each machine picks the harness its agents run in: set MEAT_MODE_AGENT_HARNESS there and restart its bridge.";

/**
 * The instruction file a machine's harness reads and the harness's name, or
 * null for a machine that has not said (an older bridge, or one not
 * connected yet), whose text names AGENTS.md or CLAUDE.md.
 */
export function instructionFile(b: Pick<Bridge, "harness"> | undefined): { file: string; harness: string } | null {
  const file = b?.harness?.instructionFile;
  return b && file ? { file, harness: harnessTitle(b) } : null;
}

/** What the add-machine form shows after the hub refused it, and whether it now asks for the password. */
export function addMachineError(e: unknown, sentPassword: boolean): { error: string; reauth: boolean } {
  const reauth = e instanceof ApiError && ((e.status === 403 && e.reauth) || e.status === 429);
  return { error: errorText(e, { reauth: sentPassword ? "Wrong password." : "" }), reauth };
}

/**
 * The password field the add-machine form gains when the hub asks for the
 * password again. prefix keeps its id apart from the other form's.
 */
export function reauthFieldHTML(prefix: string): string {
  const id = `${prefix}-password`;
  return `<input class="sr" type="text" name="username" autocomplete="username" value="meat-mode" tabindex="-1" aria-hidden="true">
<label for="${id}">Password</label><p class="muted small" id="${id}-hint">Enter your password to add a machine.</p><input id="${id}" name="password" type="password" autocomplete="current-password" required aria-describedby="${id}-hint">`;
}

/** The beads command an older machine's text names, from before machines said how each tracker raises a task. */
const BEADS_RAISE = 'bd create "Approve the footer text" -l human -d "What is needed, why, and where to find it."';

/**
 * How the text says to raise a task: one command per tracker the machine
 * runs, or the beads wording for a machine that has not said which it runs
 * (an older bridge, or one not connected yet).
 */
function raiseText(who: string, machine: Pick<Bridge, "trackers">): string {
  const trackers = (machine.trackers ?? []).filter((t) => t.raise);
  if (!trackers.length) {
    return `When you need ${who} to do something only a person can do, such as a decision, a login, a payment or a review, raise a bead labelled human in the repo you are working in:

    ${BEADS_RAISE}

It reaches ${who} when that repo is switched on in Meat Mode. Beads without the human label stay out of their list. What they do comes back to the bead: done closes it and a note becomes a comment, so check the bead before asking again.`;
  }
  const lines = trackers.map((t) => `In a repo that uses ${t.title || t.name}:\n\n    ${t.raise}`).join("\n\n");
  return `When you need ${who} to do something only a person can do, such as a decision, a login, a payment or a review, raise a task for them in the tracker of the repo you are working in.

${lines}

It reaches ${who} when that repo is switched on in Meat Mode. Tasks raised any other way stay out of their list. What they do comes back to the task: done closes it and a note becomes a comment, so check the task before asking again.`;
}

/**
 * Text for an agent instruction file (AGENTS.md, CLAUDE.md and the like) on
 * one machine: how to raise a task for the user with each tracker the
 * machine runs, or in a repo's project where it has none, and how to add to
 * their personal list. It holds no address and no token; meat-mode add
 * reads both from the bridge's config.
 */
export function agentInstructions(user: string, machine: Pick<Bridge, "name" | "trackers">): string {
  const who = user.trim() || "the user";
  return `## Tasks for ${who}

${who} gets tasks from agents in Meat Mode, a to-do app on their phone and computer. This machine is paired with it as "${machine.name}".

${raiseText(who, machine)}

In a repo with no tracker, add the task to that repo's project in Meat Mode instead, from inside the repo:

    meat-mode add "Approve the footer text" --project . --note "What is needed, why, and where to find it." --author <your name>

--project . names the project after the repo. ${who} sees it as its own list, and what they do comes back to the to-do, so check it with the commands below before asking again.

To put something on ${who}'s personal to-do list instead, not tied to a repo:

    meat-mode add "Renew the library card" --due 2026-03-02 --note "It expires on Friday." --author <your name>

--due takes a date (due at 9:00), a date and time in ${who}'s time zone such as "2026-03-02 18:30", or an RFC 3339 time. Pass --id with a UUID you make up, and running the command again changes nothing. It prints the new to-do's id, such as mm-12.

--every makes it repeat and needs --due, its first date: day, week, month or year, "3 days", weekday, mon,thu or "2 weeks on mon,thu". Done moves it to its next date; add --from-done to count from the day it is done. --until 2026-11-26 ends the repeat on that date, and the done after its last date closes it.

To change a to-do this machine added, by that id:

    meat-mode edit mm-12 --due "2026-03-09 18:30"
    meat-mode done mm-12
    meat-mode reopen mm-12

For a to-do in a project, add --project . to each, from inside the repo. edit takes --title, --description, --due or --clear-due, --every (with --from-done) or --no-repeat, --until or --no-until, and --priority (0 highest to 4, 2 is normal). These work only on to-dos added from this machine.

Keep titles short and say what to do. Put the context in the description or the note.
`;
}

/**
 * The instructions for one machine's agents, with a Copy button and, when
 * there are several machines, a choice of which. prefix keeps the ids apart
 * where Settings and the wizard are both in the page. The Copy button's act
 * is "agents-copy" and the choice's id ends in "agents-machine".
 */
export function instructionsHTML(user: string, machines: Bridge[], machine: string, prefix: string): string {
  const pick =
    machines.length > 1
      ? `<div class="agents-pick"><label for="${prefix}agents-machine">Machine</label><select id="${prefix}agents-machine" data-agents-machine>${machines
          .map((b) => `<option value="${esc(b.name)}"${b.name === machine ? " selected" : ""}>${esc(b.name)}</option>`)
          .join("")}</select></div>`
      : "";
  const bridge = machines.find((b) => b.name === machine) ?? { name: machine };
  return `${pick}<pre class="cmd agents-text" id="${prefix}agents-text" tabindex="0" aria-label="Instructions for agents on ${esc(machine)}">${esc(agentInstructions(user, bridge))}</pre>
<button class="btn agents-copy" data-act="agents-copy" data-copy="${prefix}agents-text">Copy instructions</button>`;
}

/** The machine to write instructions for: the chosen one while it is paired, else the newest. */
export function pickMachine(machines: Bridge[], chosen: string): string {
  if (machines.some((b) => b.name === chosen)) return chosen;
  return [...machines].sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0]?.name ?? "";
}
