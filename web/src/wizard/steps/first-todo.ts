import { api, newId } from "../../api.ts";
import type { AppContext } from "../../app-context.ts";
import { fmtDay, fmtTime } from "../../dates.ts";
import { esc } from "../../html.ts";
import { repoLabel } from "../../model.ts";
import { browserZone } from "../../profile.ts";
import { ADD_ERROR_TEXT, addAction, readQuickAdd, type QuickRead } from "../../quickadd.ts";
import { repeatLabel } from "../../repeat.ts";
import type { Repo } from "../../types.ts";
import { ORDER, registerStep } from "../registry.ts";

const ID = "first-todo";

/** What the user has typed; kept so a redraw does not lose it. */
let text = "";
/** The to-do this step added, once it has. */
let created: { title: string; when: string; list: string } | null = null;
let error = "";
/** A to-do is being sent; a second submit or Continue waits for it. */
let adding = false;
/** The context of the last render, for the input listener at the end. */
let liveCtx: AppContext | null = null;

function personal(ctx: AppContext): Repo | undefined {
  return ctx.state?.repos?.find((r) => r.personal);
}

function parse(ctx: AppContext, value: string): QuickRead {
  return readQuickAdd(value, ctx.now(), ctx.state?.repos ?? []);
}

/** "Tomorrow 18:00", "Fri, Oct 23 9:00": the day and the time the reminder will fire. */
function when(due: Date, now: Date): string {
  const day = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = Math.round(
    (new Date(due.getFullYear(), due.getMonth(), due.getDate()).getTime() -
      day.getTime()) /
      86400000,
  );
  const label = diff === 0 ? "Today" : diff === 1 ? "Tomorrow" : fmtDay(due);
  return `${label} ${fmtTime(due)}`;
}

/** The highlighted copy of the input and the line that says how it was read. */
function preview(
  ctx: AppContext,
  value: string,
): { backdrop: string; summary: string } {
  const { qa, target } = parse(ctx, value);
  let backdrop = "";
  let pos = 0;
  for (const t of qa.tokens) {
    backdrop +=
      esc(value.slice(pos, t.start)) +
      `<mark class="hl-${t.kind}">${esc(value.slice(t.start, t.end))}</mark>`;
    pos = t.end;
  }
  backdrop += esc(value.slice(pos)) + " ";
  if (!qa.title)
    return {
      backdrop,
      summary: `<span class="muted">Type a to-do to see how it is read.</span>`,
    };
  const chips = [
    `<span class="chip">${esc(qa.title)}</span>`,
    qa.due
      ? `<span class="chip chip-date">Due ${esc(when(qa.due, ctx.now()))}</span>`
      : `<span class="chip">No due date</span>`,
  ];
  if (qa.repeat)
    chips.push(
      `<span class="chip chip-repeat">${esc(repeatLabel(qa.repeat, ctx.now()))}</span>`,
    );
  if (qa.early)
    chips.push('<span class="chip chip-error">Ends before it starts</span>');
  chips.push(
    `<span class="chip chip-list">${esc(qa.list && target ? repoLabel(target) : "Your own list")}</span>`,
  );
  return { backdrop, summary: chips.join("") };
}

/** Adds the typed to-do to the personal list, or to the list the text names. */
async function add(ctx: AppContext): Promise<boolean> {
  if (adding) return false;
  const res = addAction(text, ctx.now(), ctx.state?.repos ?? [], {
    id: newId(),
    zone: browserZone(),
  });
  if ("error" in res) {
    error = ADD_ERROR_TEXT[res.error];
    return false;
  }
  const { qa, target } = res;
  adding = true;
  try {
    await api.postAction(res.action);
  } catch (e) {
    error = `Could not save: ${(e as Error).message}`;
    return false;
  } finally {
    adding = false;
  }
  created = {
    title: qa.title,
    when: qa.due ? when(qa.due, ctx.now()) : "",
    list: repoLabel(target),
  };
  error = "";
  text = "";
  await ctx.refresh();
  return true;
}

registerStep({
  id: ID,
  title: "First to-do",
  part: "hub",
  order: ORDER.firstTodo,
  applies: (ctx) => Boolean(personal(ctx)),
  done: () => created !== null,
  start() {
    text = "";
    created = null;
    error = "";
  },
  render(ctx) {
    liveCtx = ctx;
    const example = `Water the plants tomorrow 6pm #${repoLabel(personal(ctx)!)}`;
    const reminders = `<p class="muted">A reminder fires at the due time, or at 9:00 when only a date is given. Done and snooze work from the notification.</p>`;
    if (created) {
      const due = created.when ? ` Due ${esc(created.when)}.` : "";
      return `<p role="status" class="wiz-added">Added "${esc(created.title)}" to ${esc(created.list)}.${due}</p>
<p>It is on your list now. Use the quick add box any time to add more.</p>
<div class="btn-row"><button class="btn" data-act="first-todo-another">Add another</button></div>
${reminders}`;
    }
    const p = preview(ctx, text);
    return `<p>Quick add reads a date and a list out of what you type. Try it, or use the example.</p>
<form data-form="first-todo" class="ft-form">
<div class="qa-field"><div class="qa-backdrop" id="ft-backdrop" aria-hidden="true">${p.backdrop}</div>
<input id="ft-input" name="todo" aria-label="To-do" autocomplete="off" placeholder="${esc(example)}" value="${esc(text)}"></div>
<div class="chips" id="ft-read" aria-live="polite">${p.summary}</div>
${error ? `<p class="login-error" role="alert">${esc(error)}</p>` : ""}
<div class="btn-row"><button class="btn btn-primary" type="submit">Add to-do</button>
<button class="btn" type="button" data-act="first-todo-example" data-example="${esc(example)}">Use the example</button></div>
</form>
${reminders}`;
  },
  act(act, el, ctx) {
    if (act === "first-todo-example") {
      text = el.dataset.example ?? "";
      error = "";
      ctx.render();
      document.querySelector<HTMLInputElement>("#ft-input")?.focus();
    } else if (act === "first-todo-another") {
      created = null;
      ctx.render();
      document.querySelector<HTMLInputElement>("#ft-input")?.focus();
    }
  },
  async submit(_form, ctx) {
    await add(ctx);
    ctx.render();
  },
  // Continue adds what is typed, so a to-do the user wrote is never lost.
  async next(ctx) {
    if (created || !text.trim()) return true;
    if (await add(ctx)) return true;
    ctx.render();
    return false;
  },
});

// Typing redraws only the read-out, so the input keeps focus and its caret.
function onInput(e: Event) {
  const input = e.target as HTMLInputElement;
  if (input.id !== "ft-input" || !liveCtx) return;
  text = input.value;
  error = "";
  const p = preview(liveCtx, text);
  const backdrop = document.querySelector<HTMLElement>("#ft-backdrop");
  if (backdrop) {
    backdrop.innerHTML = p.backdrop;
    backdrop.scrollLeft = input.scrollLeft;
  }
  const read = document.querySelector<HTMLElement>("#ft-read");
  if (read) read.innerHTML = p.summary;
}

function onScroll(e: Event) {
  const input = e.target as HTMLInputElement;
  if (input.id !== "ft-input") return;
  const backdrop = document.querySelector<HTMLElement>("#ft-backdrop");
  if (backdrop) backdrop.scrollLeft = input.scrollLeft;
}

// The unit tests load the steps without a document.
if (typeof document !== "undefined") {
  document.addEventListener("input", onInput);
  document.addEventListener("scroll", onScroll, true);
}
