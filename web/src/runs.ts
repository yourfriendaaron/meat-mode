// Agent runs on items (docs/api.md, Agent runs): the line of text each state
// shows, the label of the note box's pink button, and which repos let an
// agent work. Pure: nothing here touches the page.

import { appAuthor, isAgentItem, isPersonal, repoKey } from "./model.ts";
import type { Comment, Item, Repo, Run, RunReason, RunState } from "./types.ts";

/** Why a run stopped, in plain words. */
export const REASON_TEXT: Record<RunReason, string> = {
  stopped: "You stopped it",
  "usage-limit": "It hit its usage limit",
  timeout: "It ran past its time limit",
  budget: "It spent its budget for one run",
  "run-limit": "It used up its runs on this task",
  busy: "Too many agents were running on its machine",
  "not-allowed": "Its machine does not allow agents in this repo",
  "no-reply": "It ended without a note or closing the task",
  failed: "The agent command failed",
  crashed: "The agent process quit",
  restarted: "Its machine restarted the bridge while it ran",
  offline: "Its machine stopped answering",
};

/** The agent's name for a run or an item: the run's, else the bead's creator, else "agent". */
export function agentName(item: Item): string {
  return item.run?.agent || item.createdBy || "agent";
}

export function isWorking(item: Item): boolean {
  return item.run?.state === "working";
}

/** The items with a live run, in the order the hub sent them. */
export function workingItems(items: Item[]): Item[] {
  return items.filter(isWorking);
}

/** Whether an ask may wake an agent on this item: an agent item in a repo whose agents switch is on. */
export function agentsAllowed(item: Item, repos: Repo[]): boolean {
  if (!isAgentItem(item)) return false;
  return repos.some((r) => repoKey(r) === repoKey(item) && r.agentsAllowed === true);
}

/** Whether any list lets agents work, so the New task form offers its switch. */
export function anyAgentsAllowed(repos: Repo[]): boolean {
  return repos.some((r) => !isPersonal(r) && r.agentsAllowed === true);
}

export interface RunLine {
  state: RunState;
  /** The bold lead: the agent's name, or what it did. */
  lead: string;
  /** The rest of the line, after " · "; may be empty. */
  text: string;
}

/**
 * The one line of text a run shows in its state's colour, such as
 * "planner · Shortening the second line". `full` adds the stopped run's
 * detail sentence, which the task sheet has room for and a row does not.
 */
export function runLine(run: Run, full = false): RunLine {
  const who = run.agent || "agent";
  const activity = run.activity?.trim() ?? "";
  switch (run.state) {
    case "working":
      return { state: run.state, lead: who, text: activity || "Starting" };
    case "waiting":
      return { state: run.state, lead: `${who} replied`, text: activity };
    case "done":
      return { state: run.state, lead: `${who} finished`, text: activity };
    case "stopped": {
      const why = run.reason ? REASON_TEXT[run.reason] : "It stopped before it finished";
      const detail = full && run.detail?.trim() ? ` ${run.detail.trim()}` : "";
      return { state: run.state, lead: `${who} stopped`, text: `${why}.${detail}` };
    }
  }
}

/** The line as plain text, for labels and tests. */
export function runLineText(line: RunLine): string {
  return line.text ? `${line.lead} · ${line.text}` : line.lead;
}

/** The note box's pink button, by the item's run: a follow-up while it works, a reply while it waits. */
export function askLabel(item: Item): { label: string; kind: "ask" | "send" | "reply" } {
  const who = agentName(item);
  switch (item.run?.state) {
    case "working":
      return { label: `Send to ${who}`, kind: "send" };
    case "waiting":
      return { label: `Reply to ${who}`, kind: "reply" };
    default:
      return { label: "Save and ask agent", kind: "ask" };
  }
}

/** The user's comments the live run has not reached yet: the newest run.queued of their own. */
export function queuedComments(item: Item, user: string): Set<string> {
  const n = isWorking(item) ? (item.run?.queued ?? 0) : 0;
  if (n <= 0) return new Set();
  const own = appAuthor(user);
  const mine = (item.comments ?? []).filter((c: Comment) => c.author === own);
  return new Set(mine.slice(-n).map((c) => c.id));
}

/** Whether the undo bar after Done or Won't do offers Tell <agent>. */
export function offersTell(item: Item, repos: Repo[]): boolean {
  return agentsAllowed(item, repos);
}

/** The run states that end a run, for the flash when one settles. */
export function runEnded(before: RunState | undefined, after: RunState | undefined): boolean {
  return before === "working" && after !== undefined && after !== "working";
}

/** Why an item takes no ask, for the New task switch: "" when it can. */
export function noAgentReason(list: Repo | undefined): string {
  if (!list) return "";
  if (isPersonal(list)) return "Personal to-dos have no agent.";
  if (!list.agentsAllowed) return "Agents are off for this list. They switch on at its machine.";
  return "";
}
