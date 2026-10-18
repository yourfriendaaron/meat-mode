// Agent machines, shared by Settings and the setup wizard's agent steps: the
// repo switches under each machine, the commands that install and pair the
// bridge, and the text that tells a machine's agents how to raise tasks.

import { esc } from "./html.ts";
import { HUB_BRIDGE, repoLabel } from "./model.ts";
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

/**
 * The repos a machine found, each with its sync switch. A click on a switch
 * reaches main.ts as "sync-switch". idPrefix keeps the switches' ids apart
 * where Settings and the wizard are both in the page.
 */
export function machineReposHTML(repos: Repo[], bridge: string, idPrefix = "sync-"): string {
  const list = repos.filter((r) => r.bridge === bridge).sort((a, b) => a.name.localeCompare(b.name));
  if (!list.length) return '<p class="muted small m-repos">No beads repos found on this machine yet.</p>';
  return `<ul class="m-repos" aria-label="Repos on ${esc(bridge)}">${list
    .map((r) => {
      const control = `<button class="switch" role="switch" id="${esc(`${idPrefix}${bridge}/${r.name}`)}" aria-checked="${r.optedIn}" aria-label="Sync ${esc(repoLabel(r))} (${esc(r.name)})" data-act="sync-switch" data-bridge="${esc(bridge)}" data-repo="${esc(r.name)}"><span class="switch-knob" aria-hidden="true"></span></button>`;
      const note = r.pending ? `<div class="muted small" role="status">Waiting for ${esc(bridge)}</div>` : "";
      return `<li class="m-repo"><div class="m-repo-name"><div>${esc(repoLabel(r))}</div><div class="muted small">${esc(r.name)}</div>${note}</div>${control}</li>`;
    })
    .join("")}</ul>`;
}

/**
 * Text for an agent instruction file (AGENTS.md, CLAUDE.md and the like) on
 * one machine: how to raise a task for the user and how to add to their
 * personal list. It holds no address and no token; meat-mode add reads both
 * from the bridge's config.
 */
export function agentInstructions(user: string, machine: string): string {
  const who = user.trim() || "the user";
  return `## Tasks for ${who}

${who} gets tasks from agents in Meat Mode, a to-do app on their phone and computer. This machine is paired with it as "${machine}".

When you need ${who} to do something only a person can do, such as a decision, a login, a payment or a review, raise a bead labelled human in the repo you are working in:

    bd create "Approve the footer text" -l human -d "What is needed, why, and where to find it."

It reaches ${who} when that repo is switched on in Meat Mode. Beads without the human label stay out of their list. What they do comes back to the bead: done closes it, a note becomes a comment and a snooze defers it, so check the bead before asking again.

To put something on ${who}'s personal to-do list instead, not tied to a repo:

    meat-mode add "Renew the library card" --due 2026-03-02 --note "It expires on Friday." --author <your name>

--due takes a date (due at 9:00), a date and time in ${who}'s time zone such as "2026-03-02 18:30", or an RFC 3339 time. Pass --id with a UUID you make up, and running the command again changes nothing. It prints the new to-do's id, such as mm-12.

--every makes it repeat and needs --due, its first date: day, week, month or year, "3 days", weekday, mon,thu or "2 weeks on mon,thu". Done moves it to its next date; add --from-done to count from the day it is done.

To change a to-do this machine added, by that id:

    meat-mode edit mm-12 --due "2026-03-09 18:30"
    meat-mode done mm-12
    meat-mode reopen mm-12

edit takes --title, --description, --due or --clear-due, --every (with --from-done) or --no-repeat, and --priority (0 highest to 4, 2 is normal). These work only on to-dos added from this machine.

Keep titles short and say what to do. Put the context in the description or the note.
`;
}

/** Copies the text of the element with that id, falling back to selecting it. */
export async function copyFrom(id: string): Promise<void> {
  const el = document.getElementById(id);
  if (!el) return;
  try {
    await navigator.clipboard.writeText(el.textContent ?? "");
  } catch {
    const range = document.createRange();
    range.selectNodeContents(el);
    const sel = getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
    document.execCommand("copy");
  }
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
  return `${pick}<pre class="cmd agents-text" id="${prefix}agents-text" tabindex="0" aria-label="Instructions for agents on ${esc(machine)}">${esc(agentInstructions(user, machine))}</pre>
<button class="btn agents-copy" data-act="agents-copy" data-copy="${prefix}agents-text">Copy instructions</button>`;
}

/** The machine to write instructions for: the chosen one while it is paired, else the newest. */
export function pickMachine(machines: Bridge[], chosen: string): string {
  if (machines.some((b) => b.name === chosen)) return chosen;
  return [...machines].sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0]?.name ?? "";
}
