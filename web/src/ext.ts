// Routes events main.ts does not handle itself to the setup wizard, its
// steps and the Settings sections kept in their own modules.

import type { AppContext } from "./app-context.ts";
import { attachmentHandlers } from "./attachments.ts";
import { findSection } from "./settings/registry.ts";
import { currentStep, wizardAct } from "./wizard/wizard.ts";
import "./settings/index.ts";

function owner(el: Element) {
  if (el.closest("[data-attachments]")) return attachmentHandlers;
  const step = el.closest<HTMLElement>("[data-step]");
  if (step) return currentStep(step.dataset.step);
  const section = el.closest<HTMLElement>("[data-section]");
  return section ? findSection(section.dataset.section) : undefined;
}

export async function extAct(el: HTMLElement, ctx: AppContext): Promise<void> {
  const act = el.dataset.act ?? "";
  if (await wizardAct(act, el, ctx)) return;
  await owner(el)?.act?.(act, el, ctx);
}

export async function extSubmit(form: HTMLFormElement, ctx: AppContext): Promise<void> {
  await owner(form)?.submit?.(form, ctx);
}

export function extChange(el: HTMLElement, ctx: AppContext): void {
  owner(el)?.change?.(el, ctx);
}
