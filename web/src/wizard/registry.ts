// The setup wizard's steps. Each step is one module in ./steps that calls
// registerStep; ./steps/index.ts imports them all.

import type { AppContext, Handlers } from "../app-context.ts";
import { partsOf, type Mode, type Part, type Progress } from "./rules.ts";

/** Where each step of the epic sits in a run. Steps sort by this, not by import order. */
export const ORDER = {
  welcome: 0,
  connection: 10,
  secure: 20,
  profile: 30,
  login: 40,
  install: 50,
  reminders: 60,
  appearance: 70,
  firstTodo: 80,
  agents: 90,
  finish: 1000,
} as const;

/** The run on screen, handed to each step's render. */
export interface Run {
  mode: Mode;
  /** The steps of this run, in order, Welcome and Finish included. */
  steps: Step[];
  progress: Progress;
}

export interface Step extends Handlers {
  /** Unique and kebab-case; saved in the progress, so keep it stable. */
  id: string;
  /** The step's heading, and its name in the step list and on Finish. */
  title: string;
  /** The part it belongs to; null for Welcome and Finish, which every run has. */
  part: Part | null;
  /** Its place in a run: a value from ORDER. */
  order: number;
  /** Whether the step belongs in the run at all, such as Log in only when the hub has a login. Default yes. */
  applies?(ctx: AppContext): boolean;
  /** The step's body under its heading, as HTML. Escape every value with esc() from ../html.ts. */
  render(ctx: AppContext, run: Run): string;
  /** Whether what the step sets up is already in place; Finish then lists it as set up even when skipped. */
  done?(ctx: AppContext): boolean;
  /** Whether Skip is offered. Default yes; Welcome and Finish say no. */
  skippable?: boolean;
  /** The label of the Continue button. Default "Continue", or "Done" on the last step. */
  continueLabel?: string;
  /** Runs on Continue. Resolve false to stay on the step, such as when a form it holds is not valid yet. */
  next?(ctx: AppContext): boolean | Promise<boolean>;
  /** Runs when a new run starts, so a step that keeps state in its module can forget the last run. */
  start?(): void;
  /** Runs when the step is left by Skip or Back, or by closing the run, so it can drop what must not outlive the visit, such as a one-time token. */
  leave?(): void;
  /** The id of the heading of its section in Settings, such as "s-push"; Finish links to it. */
  settings?: string;
}

const registered: Step[] = [];

export function registerStep(step: Step): void {
  if (registered.some((s) => s.id === step.id)) throw new Error(`setup step ${step.id} is registered twice`);
  registered.push(step);
}

export function findStep(id: string | undefined): Step | undefined {
  return registered.find((s) => s.id === id);
}

/** The steps of a run in this mode, in order, leaving out those that do not apply. */
export function stepsFor(mode: Mode, ctx: AppContext): Step[] {
  const parts = partsOf(mode);
  return registered
    .filter((s) => (s.part === null || parts.includes(s.part)) && (s.applies?.(ctx) ?? true))
    .sort((a, b) => a.order - b.order);
}

/** How a step ended in this run, as Finish lists it. */
export function stepOutcome(step: Step, progress: Progress, ctx: AppContext): "done" | "skipped" | "open" {
  if (step.done?.(ctx) || progress.done.includes(step.id)) return "done";
  if (progress.skipped.includes(step.id)) return "skipped";
  return "open";
}
