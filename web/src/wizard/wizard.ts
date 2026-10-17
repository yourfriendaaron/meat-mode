// The setup wizard's frame: the step list, Back, Skip and Continue, the
// banner that offers or resumes setup beside the list, and recording a run
// as done or dismissed. It is a view like Settings, so the list stays one
// tap away. Steps live in ./steps.

import type { AppContext } from "../app-context.ts";
import { api } from "../api.ts";
import { esc } from "../html.ts";
import { remindersOn } from "../push.ts";
import type { SetupInfo } from "../types.ts";
import { findStep, stepsFor, type Run, type Step } from "./registry.ts";
import {
  deviceRecord,
  firstRun,
  loadProgress,
  partsOf,
  saveProgress,
  setDeviceRecord,
  type Mode,
  type Progress,
} from "./rules.ts";
import "./steps/index.ts";

let info: SetupInfo | null = null;
let progress: Progress | null = null;
/** Offer this device's part in the banner. */
let offerDevice = false;
let busy = false;
/** Move focus to the step's heading after the next render. */
let focusStep = false;

/** GET /api/setup as last loaded, or null. */
export function setupInfo(): SetupInfo | null {
  return info;
}

/** Loads GET /api/setup again; keeps the last answer when the hub cannot be reached. */
export async function reloadSetup(): Promise<void> {
  try {
    info = await api.setup();
  } catch {
    // keep what we had
  }
}

/**
 * Applies the first-run rules once the app is up: resume, open on a new hub,
 * or offer this device's part. A user who moved to another view while the
 * hub answered stays there, with the banner to continue.
 */
export async function initSetup(ctx: AppContext): Promise<void> {
  const view = ctx.view;
  await reloadSetup();
  const show = ctx.view === view;
  const saved = loadProgress();
  const what = firstRun(info?.status ?? null, deviceRecord(), remindersOn(), saved);
  if (what === "resume") {
    progress = saved;
    focusStep = show;
    if (show) ctx.setView("setup");
    else ctx.render();
    return;
  }
  if (saved) saveProgress(null);
  if (what === "first") {
    startWizard("first", ctx, show);
    return;
  }
  offerDevice = what === "device";
  ctx.render();
}

/** Starts a run from its first step, and shows it unless show is false. */
export function startWizard(mode: Mode, ctx: AppContext, show = true): void {
  const steps = stepsFor(mode, ctx);
  progress = { mode, step: steps[0]?.id ?? "", done: [], skipped: [] };
  saveProgress(progress);
  offerDevice = false;
  focusStep = show;
  if (show) ctx.setView("setup");
  else ctx.render();
}

function run(ctx: AppContext): { run: Run; index: number } | null {
  if (!progress) return null;
  const steps = stepsFor(progress.mode, ctx);
  if (!steps.length) return null;
  let index = steps.findIndex((s) => s.id === progress!.step);
  if (index < 0) {
    // The saved step no longer applies: go on from the first step not yet left.
    index = Math.max(0, steps.findIndex((s) => !progress!.done.includes(s.id) && !progress!.skipped.includes(s.id)));
  }
  return { run: { mode: progress.mode, steps, progress }, index };
}

function go(step: Step | undefined, ctx: AppContext) {
  if (!progress || !step) return;
  progress.step = step.id;
  saveProgress(progress);
  focusStep = true;
  ctx.render();
}

function leave(id: string, as: "done" | "skipped") {
  if (!progress) return;
  progress.done = progress.done.filter((x) => x !== id);
  progress.skipped = progress.skipped.filter((x) => x !== id);
  progress[as].push(id);
}

/** Records the run as done for each part it holds, and closes it. */
async function complete(ctx: AppContext) {
  if (!progress) return;
  const parts = partsOf(progress.mode);
  if (parts.includes("hub")) {
    try {
      await api.recordSetup("done");
      if (info) info.status = "done";
    } catch (e) {
      ctx.toast(`Could not save the setup: ${(e as Error).message}`);
      return;
    }
  }
  if (parts.includes("device")) setDeviceRecord("done");
  close(ctx);
}

/** Records the run as dismissed, unless its part was done before, and closes it. */
async function dismiss(ctx: AppContext) {
  if (!progress) return;
  const parts = partsOf(progress.mode);
  if (parts.includes("hub") && info?.status !== "done") {
    try {
      await api.recordSetup("dismissed");
      if (info) info.status = "dismissed";
    } catch (e) {
      ctx.toast(`Could not save the setup: ${(e as Error).message}`);
    }
  }
  if (parts.includes("device") && deviceRecord() !== "done") setDeviceRecord("dismissed");
  close(ctx);
}

function close(ctx: AppContext) {
  progress = null;
  saveProgress(null);
  offerDevice = false;
  if (ctx.view === "setup") ctx.setView("today");
  else ctx.render();
}

/** Handles the frame's own data-act values; false for any other. */
export async function wizardAct(act: string, el: HTMLElement, ctx: AppContext): Promise<boolean> {
  if (!act.startsWith("wizard-")) return false;
  if (busy) return true;
  const r = run(ctx);
  const step = r?.run.steps[r.index];
  busy = true;
  try {
    switch (act) {
      case "wizard-next":
        if (!r || !step) break;
        if (step.next && !(await step.next(ctx))) break;
        if (r.index === r.run.steps.length - 1) {
          await complete(ctx);
          break;
        }
        leave(step.id, "done");
        go(r.run.steps[r.index + 1], ctx);
        break;
      case "wizard-skip":
        if (!r || !step) break;
        leave(step.id, "skipped");
        go(r.run.steps[r.index + 1], ctx);
        break;
      case "wizard-back":
        if (r) go(r.run.steps[r.index - 1], ctx);
        break;
      case "wizard-close":
      case "wizard-later":
        if (progress) await dismiss(ctx);
        else {
          setDeviceRecord("dismissed");
          offerDevice = false;
          ctx.render();
        }
        break;
      case "wizard-resume":
        focusStep = true;
        ctx.setView("setup");
        break;
      case "wizard-device":
        startWizard("device", ctx);
        break;
      case "wizard-settings":
        ctx.openSettings(el.dataset.section);
        break;
    }
  } finally {
    busy = false;
  }
  return true;
}

/** The step on screen, for events inside it. */
export function currentStep(id: string | undefined): Step | undefined {
  return progress ? findStep(id) : undefined;
}

/** The wizard view's HTML. */
export function wizardHTML(ctx: AppContext): string {
  const r = run(ctx);
  if (!r) return '<p class="empty">Setup is closed. Run it again from Settings.</p>';
  const { run: rn, index } = r;
  const step = rn.steps[index];
  const last = index === rn.steps.length - 1;
  const list = rn.steps
    .map((s, i) => {
      const state =
        i === index ? "" : rn.progress.done.includes(s.id) ? ", done" : rn.progress.skipped.includes(s.id) ? ", skipped" : "";
      const cls = i === index ? "current" : rn.progress.done.includes(s.id) ? "done" : rn.progress.skipped.includes(s.id) ? "skipped" : "";
      return `<li class="${cls}"${i === index ? ' aria-current="step"' : ""}>${esc(s.title)}${state ? `<span class="sr">${state}</span>` : ""}</li>`;
    })
    .join("");
  const back = index > 0 ? '<button class="btn" data-act="wizard-back">Back</button>' : "";
  const skip = !last && step.skippable !== false ? '<button class="btn btn-ghost" data-act="wizard-skip">Skip</button>' : "";
  const label = step.continueLabel ?? (last ? "Done" : "Continue");
  return `<div class="wizard">
<nav class="wiz-side" aria-label="Setup steps"><ol class="wiz-steps">${list}</ol></nav>
<section class="wiz-main" aria-labelledby="wiz-h">
<p class="wiz-count muted small">Step ${index + 1} of ${rn.steps.length}</p>
<div class="wiz-bar" aria-hidden="true"><div class="wiz-bar-fill" style="width:${Math.round(((index + 1) / rn.steps.length) * 100)}%"></div></div>
<div class="wiz-step" data-step="${esc(step.id)}"><h2 id="wiz-h" tabindex="-1">${esc(step.title)}</h2>${step.render(ctx, rn)}</div>
<div class="wiz-nav">${back}<span class="wiz-gap"></span>${skip}<button class="btn btn-primary" data-act="wizard-next">${esc(label)}</button></div>
${last ? "" : '<button class="link-btn wiz-close" data-act="wizard-close">Close setup</button>'}
</section>
</div>`;
}

/** Call after the wizard view was written: moves focus to a newly shown step. */
export function wizardShown(): void {
  if (!focusStep) return;
  focusStep = false;
  document.getElementById("wiz-h")?.focus({ preventScroll: true });
}

/** The banner above the list that resumes a run or offers this device's part; "" for none. */
export function bannerHTML(ctx: AppContext): string {
  if (ctx.view === "setup" || ctx.view === "settings") return "";
  if (progress) {
    return `<p>Setup is not finished.</p><div class="banner-actions"><button class="btn btn-small btn-primary" data-act="wizard-resume">Continue setup</button><button class="btn btn-small btn-ghost" data-act="wizard-later">Not now</button></div>`;
  }
  if (offerDevice && !remindersOn()) {
    return `<p>Get reminders on this device.</p><div class="banner-actions"><button class="btn btn-small btn-primary" data-act="wizard-device">Set up this device</button><button class="btn btn-small btn-ghost" data-act="wizard-later">Not now</button></div>`;
  }
  return "";
}
