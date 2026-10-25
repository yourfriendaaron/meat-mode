// Meat Mode app: state, rendering and event handling. No framework: each region
// is a render function returning an HTML string, written only when it changed,
// and one delegated handler per event type.

import { ApiError, api, errorText, newId, setUnauthorizedHandler } from "./api.ts";
import { createActions, inverse, movedOn, type Handle } from "./actions.ts";
import { MAX_REPEAT_INTERVAL, type Action, type Comment, type Edit, type Item, type NewAction, type Occurrence, type Payload, type Repo, type RunState, type Session, type State } from "./types.ts";
import {
  DAY_CAPACITY,
  type Capability,
  agentNewCount,
  can,
  attention,
  buildView,
  dateChip,
  isAgentItem,
  isDeleted,
  isOpen,
  isPersonal,
  isUnreadComment,
  isWontDo,
  openSubtasks,
  openToday,
  parentOf,
  parentTitle,
  pastDue,
  repoColours,
  repoDetail,
  repoKey,
  repoLabel,
  subtaskChip,
  subtasksFirst,
  subtasksOf,
  syncStatus,
  tomorrowMorning,
  visibleRepos,
  appAuthor,
  repoCan,
  type Group,
  type ViewId,
} from "./model.ts";
import { fmtAgo, fmtWhen, fmtWhenAt, fromYmd, startOfDay, toLocalInput, ymd } from "./dates.ts";
import { addMonths, monthEntries, monthHTML, monthOf, monthSpan, viewSwitchHTML, type MonthEntry } from "./month.ts";
import { canMove, decideMove, type EntryKind, type MoveEntry } from "./move.ts";
import { completedGroups, deletedGroups } from "./history.ts";
import { searchGroups } from "./search.ts";
import { hasTag, shownTags } from "./tags.ts";
import { bulkDismissDone, bulkDismissLabel, bulkToast, canSelect, keepPickable, pickableKeys } from "./bulk.ts";
import { fmtSnoozeTime, snoozeOptions, typedSnooze } from "./snooze.ts";
import { ADD_ERROR_TEXT, addAction, readQuickAdd, wakesAgent, type QuickAdd } from "./quickadd.ts";
import { repeatLabel } from "./repeat.ts";
import { countFromHint, fitWeekdays, readTyped, scheduleCard, scheduleReadout, scheduleSave } from "./schedule.ts";
import { play } from "./sound.ts";
import { leadSelect, loadLead } from "./lead.ts";
import { browserZone } from "./profile.ts";
import {
  TEXT_MAX,
  TITLE_MAX,
  canEdit,
  diffEdit,
  draftOf,
  dueLabel,
  editError,
  moveTargets,
  priorityChip,
  priorityOptions,
  type EditDraft,
} from "./edit.ts";
import { applyTheme, getTheme, setTheme } from "./theme.ts";
import { gate, loginErrorText } from "./login.ts";
import { claimHTML, claimScreen } from "./password.ts";
import { esc } from "./html.ts";
import { agentsOnHTML } from "./agents.ts";
import { copyText } from "./clipboard.ts";
import { ICON } from "./icons.ts";
import { remote } from "./remote.ts";
import { agentName, agentsAllowed, askLabel, isWorking, noAgentReason, offersTell, queuedComments, runEnded, runLine, workingItems } from "./runs.ts";
import { initPush, remindersOn, subscribePush } from "./push.ts";
import type { AppContext } from "./app-context.ts";
import { attachmentsHTML } from "./attachments.ts";
import { extAct, extChange, extSubmit } from "./ext.ts";
import { renderMarkdown } from "./markdown.ts";
import { trackKeyboard } from "./keyboard.ts";
import { loadStatusIfStale, publicDnsWarningHTML } from "./connection.ts";
import { leaveSettings, sectionsHTML } from "./settings/registry.ts";
import { bannerHTML, initSetup, reloadSetup, setupInfo, wizardHTML, wizardShown } from "./wizard/wizard.ts";

/** Completed and Deleted show the closed items the hub keeps for 7 days. Month is Calendar's grid, upcoming its list. */
type View = ViewId | "month" | "completed" | "deleted" | "settings" | "setup";

/** Views that show a page in place of the task list. */
function isPage(v: View): v is "settings" | "setup" {
  return v === "settings" || v === "setup";
}

interface Toast {
  id: number;
  text: string;
  undo?: () => Promise<void>;
  /** The agent item just closed, whose agent the bar offers to tell. */
  tell?: string;
}

/** The title, details or tags of a personal item, while they are a field. */
interface Inline {
  key: string;
  field: "title" | "description" | "tags";
  text: string;
  error: string;
}

/** The Schedule page: the draft it saves, and the draft the controls last made, which the typed field starts from. */
interface SchedulePage {
  kind: "schedule";
  key: string;
  draft: EditDraft;
  base: EditDraft;
  typed: string;
  typedError: string;
  error: string;
  /** Set while it asks whether a moved repeating task moves just this once. */
  ask: { set: Edit; until: Date } | null;
}

/** A page that takes the sheet's place, never a second sheet on top of it. */
type SheetPage = SchedulePage | { kind: "snooze"; key: string };

const UNDO_MS = 6000;
const SWIPE_THRESHOLD = 88;
/** How long a done row takes to slide off, then to fold shut. */
const SLIDE_MS = 280;
const FOLD_MS = 180;
/** How long a touch holds a month entry before its Move dialog opens, and how far it may wander meanwhile. */
const LONG_PRESS_MS = 500;
const LONG_PRESS_SLOP = 10;
const PHONE_MAX = 880;

const S = {
  state: null as State | null,
  offset: 0,
  loadError: "",
  view: "today" as View,
  /** The search field's text; while it holds a word, the list shows search results in place of the view. */
  query: "",
  /**
   * The tag the list is filtered by, or null. Like a search, it shows items
   * from every list in place of the view, and picking a view ends it.
   */
  tag: null as string | null,
  /** Select mode: rows pick instead of open, and the bulk bar acts on the picks. */
  selecting: false,
  picked: new Set<string>(),
  selected: null as string | null,
  open: null as string | null,
  openSeenAt: null as string | null | undefined,
  drafts: new Map<string, string>(),
  /** The page shown in the sheet in place of the task. */
  page: null as SheetPage | null,
  /** The personal item's field being changed in place. */
  inline: null as Inline | null,
  /** The sheet's More menu. */
  moreOpen: false,
  /** The note being edited, on a personal item. */
  noteEdit: null as { key: string; commentId: string; text: string } | null,
  /** Unsent text of each item's add-a-sub-task field. */
  subDrafts: new Map<string, string>(),
  snoozeKeys: null as string[] | null,
  /** The month on screen and its selected day, both local midnights; set when the month first shows. */
  month: null as { month: Date; selected: Date } | null,
  /** The open Move dialog: the month entry it moves, and why the day picked was refused. */
  move: null as { key: string; kind: EntryKind; at: string; error: string } | null,
  syncOpen: false,
  /** The phone's menu drawer, which holds the sidebar. */
  menuOpen: false,
  qaOpen: false,
  /** Whether Today shows the Later section's rows; folded, it shows one summary line. */
  laterOpen: false,
  toast: null as Toast | null,
  auth: null as Session | null,
  loginError: "",
  loggingIn: false,
};

/** Whether the app's shell is on screen. The login screen replaces it, and nothing renders into it meanwhile. */
let shellUp = false;

const $ = <T extends HTMLElement = HTMLElement>(sel: string, root: ParentNode = document) => root.querySelector<T>(sel)!;

function now(): Date {
  return new Date(Date.now() + S.offset);
}

function items(): Item[] {
  return S.state?.items ?? [];
}
function repos(): Repo[] {
  return S.state?.repos ?? [];
}
function user(): string {
  return S.state?.user ?? "";
}
function byKey(key: string | null): Item | undefined {
  return key ? items().find((i) => i.key === key) : undefined;
}
/** Whether the item's list can apply `kind`; see can in model.ts. */
function allows(item: Item, kind: Capability): boolean {
  return can(item, kind, repos());
}
function isPhone(): boolean {
  return ($("#app").clientWidth || window.innerWidth) < PHONE_MAX;
}

const cache = new WeakMap<Element, string>();
/** Set while setHTML rewrites a region, so the blur of a field it removes is not a user's. */
let writing = false;
/**
 * Writes a region only when its HTML changed. Typed values and focus of
 * inputs with an id survive the rewrite, since values never live in the HTML.
 */
function setHTML(el: Element, html: string): boolean {
  if (cache.get(el) === html) return false;
  cache.set(el, html);
  const kept = new Map<string, string>();
  for (const f of el.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input[id], textarea[id]")) kept.set(f.id, f.value);
  const active = document.activeElement;
  const focusId = active && el.contains(active) && active.id ? active.id : "";
  const sel =
    active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement
      ? [active.selectionStart, active.selectionEnd]
      : null;
  writing = true;
  try {
    el.innerHTML = html;
  } finally {
    writing = false;
  }
  for (const [id, value] of kept) {
    const f = el.querySelector<HTMLInputElement | HTMLTextAreaElement>(`#${CSS.escape(id)}`);
    if (f && !f.value && value) f.value = value;
  }
  if (focusId) {
    const f = el.querySelector<HTMLElement>(`#${CSS.escape(focusId)}`);
    f?.focus({ preventScroll: true });
    if (sel && (f instanceof HTMLInputElement || f instanceof HTMLTextAreaElement)) {
      try {
        f.setSelectionRange(sel[0], sel[1]);
      } catch {
        // not a text field
      }
    }
  }
  return true;
}

/** The point of light that runs round a working run's edge; CSS draws it. */
const TRACER = '<span class="run-fx" aria-hidden="true"></span>';

// ---------- views ----------

function viewTitle(v: View): string {
  switch (v) {
    case "today":
      return "Today";
    case "upcoming":
    case "month":
      return "Calendar";
    case "agents":
      return "From agents";
    case "all":
      return "Everything";
    case "completed":
      return "Completed";
    case "deleted":
      return "Deleted";
    case "settings":
      return "Settings";
    case "setup":
      return "Setup";
    default: {
      const r = repos().find((x) => `list:${repoKey(x)}` === v);
      return r ? listLabel(r) : "List";
    }
  }
}

/** The repo's name for chips and pickers, with its path where names repeat. */
function listLabel(r: Repo): string {
  const detail = repoDetail(r, visibleRepos(repos()));
  return detail ? `${repoLabel(r)} · ${detail}` : repoLabel(r);
}

/** The repo's name for the sidebar: the path goes on a second line. */
function listLabelHTML(r: Repo): string {
  const detail = repoDetail(r, visibleRepos(repos()));
  return esc(repoLabel(r)) + (detail ? `<small class="nav-sub">${esc(detail)}</small>` : "");
}

/** Whether the list shows search results: a word in the search field, on a task view. */
function searching(): boolean {
  return !isPage(S.view) && S.query.trim() !== "";
}

/** Whether the list shows the items with one tag, from every list. */
function filtering(): boolean {
  return !isPage(S.view) && S.tag !== null;
}

function currentGroups(): Group[] {
  if (isPage(S.view)) return [];
  const tag = S.tag;
  if (S.query.trim()) return searchGroups(items(), S.query, now(), tag === null ? undefined : (i) => hasTag(i, tag));
  if (tag !== null) return buildView("all", items().filter((i) => hasTag(i, tag)), now());
  if (S.view === "completed") return completedGroups(items(), now());
  if (S.view === "deleted") return deletedGroups(items());
  // The month draws its own grid.
  if (S.view === "month") return [];
  return buildView(S.view, items(), now());
}

/** Keys of the rows in the current view, in display order. */
function visibleKeys(): string[] {
  return currentGroups().flatMap((g) => g.items.map((i) => i.key));
}

// ---------- render: chrome ----------

function renderNav() {
  const all = items();
  const n = now();
  const agentNew = agentNewCount(all, user());
  // While any agent works, its tab icon carries the same light as the run's row.
  const working = workingItems(all).length;
  const agentsIcon = working ? `<span class="tab-ic" data-run="working">${TRACER}${ICON.agents}</span>` : ICON.agents;
  const agentsWorking = working ? `<span class="sr">, ${working === 1 ? "an agent is" : `${working} agents are`} working</span>` : "";
  const upcoming = buildView("upcoming", all, n).reduce((s, g) => s + g.items.length, 0);
  const everything = all.filter(isOpen).length;
  // Calendar opens on the month; its List shows the same items as a list, so Calendar stays current.
  const current = S.view === "upcoming" ? "month" : S.view;
  const entry = (v: View, label: string, icon: string, count: string) =>
    `<button class="nav-item" data-act="view" data-view="${esc(v)}"${current === v ? ' aria-current="page"' : ""}>${icon}<span class="nav-label">${label}</span>${count}</button>`;
  const plain = (c: number) => (c ? `<span class="count">${c}</span>` : "");
  setHTML(
    $("#nav"),
    entry("today", "Today", ICON.today, plain(openToday(all, n))) +
      entry("month", "Calendar", ICON.upcoming, plain(upcoming)) +
      entry("agents", "From agents", agentsIcon, agentsWorking + (agentNew ? `<span class="badge-new">${agentNew} new</span>` : "")) +
      entry("all", "Everything", ICON.all, plain(everything)) +
      entry("completed", "Completed", ICON.check, "") +
      entry("deleted", "Deleted", ICON.trash, ""),
  );

  const colours = repoColours(repos());
  setHTML(
    $("#lists"),
    visibleRepos(repos())
      .map((r) => {
        const key = repoKey(r);
        const count = all.filter((i) => isOpen(i) && repoKey(i) === key).length;
        return entry(`list:${key}`, listLabelHTML(r), `<span class="dot c${colours.get(key) ?? 0}" aria-hidden="true"></span>`, plain(count));
      })
      .join("") || '<p class="muted small">No lists yet.</p>',
  );

  setHTML(
    $("#tabs"),
    [
      ["today", "Today", ICON.today, ""],
      ["month", "Calendar", ICON.upcoming, ""],
      ["agents", "Agents", agentsIcon, agentsWorking + (agentNew ? `<span class="tab-badge" aria-label="${agentNew} new">${agentNew}</span>` : "")],
    ]
      .map(([v, label, icon, badge]) => {
        return `<button class="tab" data-act="view" data-view="${v}"${current === v ? ' aria-current="page"' : ""}>${icon}<span>${label}</span>${badge}</button>`;
      })
      .join(""),
  );
}

/** The phone opens the sidebar as a drawer from the menu button. */
function renderMenu() {
  if (!isPhone()) S.menuOpen = false;
  $("#app").classList.toggle("menu-open", S.menuOpen);
  $(".menu-btn").setAttribute("aria-expanded", String(S.menuOpen));
}

function openMenu() {
  S.menuOpen = true;
  render();
  $<HTMLElement>("#sidebar .nav-item")?.focus();
}

function closeMenu() {
  S.menuOpen = false;
  render();
  $<HTMLElement>(".menu-btn").focus();
}

function renderSync() {
  if (!shellUp) return;
  const st = S.state;
  const status = st ? syncStatus(st, now()) : { tone: "waiting" as const, text: S.loadError ? "Hub unreachable" : "Loading" };
  const html = `<span class="pill-dot" aria-hidden="true"></span><span>${esc(status.text)}</span>`;
  for (const el of document.querySelectorAll<HTMLElement>(".sync-pill")) {
    el.dataset.tone = status.tone;
    el.setAttribute("aria-expanded", String(S.syncOpen));
    setHTML(el, html);
  }
  const pop = $("#syncpop");
  pop.hidden = !S.syncOpen;
  if (!S.syncOpen) return;
  const list = syncActions.peek();
  let body: string;
  if (syncActions.error()) body = '<p class="muted">Could not load actions.</p>';
  else if (!list) body = '<p class="muted">Loading</p>';
  else if (!list.length) body = '<p class="muted">No actions yet.</p>';
  else
    body =
      '<ul class="actions">' +
      list
        .map((a) => {
          const item = byKey(`${a.bridge}/${a.repo}/${a.beadId ?? ""}`);
          const title = a.kind === "add" ? a.payload.title : a.kind === "sync" || a.kind === "agents" ? `${a.repo} on ${a.bridge}` : a.kind === "limits" ? a.bridge : (item?.title ?? a.beadId ?? "");
          const state =
            a.status === "applied" ? (isPersonal(a) ? "Saved" : "Synced") : a.status === "pending" ? "Waiting" : a.status === "cancelled" ? "Cancelled" : `Failed: ${a.error ?? ""}`;
          // An agent's change names the agent and its machine.
          const by = a.machine ? ` · ${a.author === a.machine ? a.machine : `${a.author} on ${a.machine}`}` : "";
          return `<li class="act act-${esc(a.status)}"><span class="act-what"><span class="act-kind">${esc(actionLabel(a))}</span> ${esc(title)}</span><span class="act-state">${esc(state)}${esc(by)} · ${esc(fmtAgo(new Date(a.createdAt), now()))}</span></li>`;
        })
        .join("") +
      "</ul>";
  setHTML(
    pop,
    `<div class="pop-head"><h2 id="syncpop-h">Recent actions</h2><button class="icon-btn" data-act="sync-close" aria-label="Close">${ICON.close}</button></div>${body}`,
  );
}

function actionLabel(a: Action): string {
  switch (a.kind) {
    case "done":
      return "Done";
    case "reopen":
      return "Reopen";
    case "dismiss":
      return isPersonal(a) ? "Delete" : "Won't do";
    case "note":
      return "Note on";
    case "add":
      return "Add";
    case "sync":
      return a.payload.on ? "Sync on:" : "Sync off:";
    case "ask":
      return "Ask agent on";
    case "stop":
      return "Stop agent on";
    case "agents":
      return a.payload.on ? "Agents on:" : "Agents off:";
    case "limits":
      return "Limits on";
    case "snooze":
      return a.payload.until ? `Snooze to ${fmtWhen(new Date(a.payload.until), now())}:` : "Clear snooze on";
    case "edit":
      return "Edit";
    case "note-edit":
      return "Edit note on";
    case "note-delete":
      return "Delete note on";
    case "note-restore":
      return "Restore note on";
  }
}

// ---------- render: list ----------

/**
 * An item's chips. In the detail (`detail`) a tag chip is a button that
 * filters the list by it; on a row it sits inside the row's button, so it is
 * text that a tap on it routes to the same filter. A row shows New and the
 * date apart from its chips: see attentionDot and whenHTML.
 */
function chipsFor(item: Item, opts: { list: boolean; nested?: boolean; detail?: boolean }): string {
  const n = now();
  const agent = isAgentItem(item);
  const out: string[] = [];
  // The list comes first, so it starts at the same x on every row.
  if (opts.list) {
    const r = repos().find((x) => repoKey(x) === repoKey(item));
    const colour = repoColours(repos()).get(repoKey(item)) ?? 0;
    out.push(`<span class="chip chip-list"><span class="dot c${colour}" aria-hidden="true"></span>${esc(r ? listLabel(r) : item.repo)}</span>`);
  }
  if (opts.detail) {
    const att = attention(item, user());
    if (att) out.push(`<span class="chip chip-new">${ATTENTION_TEXT[att]}</span>`);
  }
  const prio = priorityChip(item.priority);
  if (prio) out.push(`<span class="chip ${item.priority === 0 ? "chip-urgent" : "chip-high"}">${esc(prio)}</span>`);
  if (opts.detail) out.push(whenHTML(item, "chip"));
  if (item.repeat) out.push(`<span class="chip chip-repeat">${ICON.repeat}${esc(repeatLabel(item.repeat, n))}</span>`);
  if (isWontDo(item)) out.push('<span class="chip chip-wontdo">Won\'t do</span>');
  if (agent && item.createdBy) out.push(`<span class="chip chip-agent">${esc(item.createdBy)}</span>`);
  const notes = item.comments?.length ?? 0;
  if (notes) out.push(`<span class="chip chip-notes" aria-label="${notes} ${notes === 1 ? "note" : "notes"}">${ICON.note}${notes}</span>`);
  const sc = subtaskChip(item, items());
  if (sc) {
    const [done, all] = sc.split("/");
    out.push(`<span class="chip chip-subtasks" aria-label="Sub-tasks: ${done} of ${all} done">${ICON.subtask}${sc}</span>`);
  }
  // A sub-task on its own row names its parent; under the parent it needs not.
  const pt = item.parent && !opts.nested ? parentTitle(item, items()) : "";
  if (pt) out.push(`<span class="chip chip-parent">${ICON.subtask}${esc(`Part of ${pt}`)}</span>`);
  for (const t of shownTags(item)) {
    out.push(
      opts.detail
        ? `<button type="button" class="chip chip-tag" data-act="tag" data-tag="${esc(t)}" aria-label="${esc(`Show items tagged ${t}`)}">${ICON.tag}${esc(t)}</button>`
        : `<span class="chip chip-tag" data-act="tag" data-tag="${esc(t)}">${ICON.tag}${esc(t)}</span>`,
    );
  }
  return out.join("");
}

const ATTENTION_TEXT = { new: "New", reply: "New reply" } as const;

/** A row's New or New reply: a dot before the title, its words there for screen readers. */
function attentionDot(item: Item): string {
  const att = attention(item, user());
  return att ? `<span class="chip chip-new attn"><span class="attn-text">${ATTENTION_TEXT[att]}</span></span>` : "";
}

/** The item's date, time or snooze, in its tone; a row shows it in a column of its own. */
function whenHTML(item: Item, cls: string): string {
  const dc = dateChip(item, now());
  return dc ? `<span class="${cls} chip-${dc.tone}">${dc.snoozed ? ICON.moon : ""}${esc(dc.text)}</span>` : "";
}

function checkButton(item: Item, agent: boolean): string {
  const open = isOpen(item);
  if (open && S.selecting) {
    const on = S.picked.has(item.key);
    return `<button class="check pick${agent ? " check-agent" : ""}${on ? " checked" : ""}" role="checkbox" aria-checked="${on}" data-act="pick" data-key="${esc(item.key)}" aria-label="${esc(`Select: ${item.title}`)}">${ICON.check}</button>`;
  }
  // A list that cannot mark done or reopen keeps the check's place, empty.
  if (!allows(item, open ? "done" : "reopen")) return `<span class="check check-none${agent ? " check-agent" : ""}${open ? "" : " checked"}" aria-hidden="true"></span>`;
  const gone = isDeleted(item);
  const label = open ? `Mark done: ${item.title}` : `${gone ? "Restore" : "Reopen"}: ${item.title}`;
  return `<button class="check${agent ? " check-agent" : ""}${open ? "" : " checked"}" data-act="${open ? "done" : gone ? "restore" : "reopen"}" data-key="${esc(item.key)}" aria-label="${esc(label)}">${ICON.check}</button>`;
}

function statusLine(item: Item): string {
  if (item.error) return `<span class="row-error" role="status">${esc(item.error)}</span>`;
  if (item.pending) return '<span class="row-pending">Waiting for sync</span>';
  return "";
}

/**
 * A run's line of text in its state's colour. The state is in the words too,
 * so the colour and the light are never the only sign of it.
 */
function runLineHTML(item: Item, full: boolean): string {
  if (!item.run) return "";
  const line = runLine(item.run, full);
  return `<span class="run-line" data-run="${esc(line.state)}"><b>${esc(line.lead)}</b>${line.text ? ` · ${esc(line.text)}` : ""}</span>`;
}

/** The surface attributes of an element that shows a run's state: data-run, and the flash while a run has just ended. */
function runAttrs(item: Item): string {
  if (!item.run) return "";
  return ` data-run="${esc(item.run.state)}"${arrived.has(item.key) ? " data-arrive" : ""}`;
}

/** Items whose run just ended, so their surfaces flash once. */
const arrived = new Set<string>();
/** Each item's run state at the last load, to see a run end. */
const lastRun = new Map<string, RunState | undefined>();

/** Notes the runs that ended since the last load; their cards flash once. */
function noteRunEnds(next: Item[]) {
  const first = lastRun.size === 0;
  for (const it of next) {
    const before = lastRun.get(it.key);
    if (!first && runEnded(before, it.run?.state)) {
      arrived.add(it.key);
      setTimeout(() => {
        arrived.delete(it.key);
        render();
      }, 1400);
    }
  }
  lastRun.clear();
  for (const it of next) lastRun.set(it.key, it.run?.state);
}

function rowHTML(item: Item, showList: boolean, nested = false): string {
  const agent = isAgentItem(item);
  const done = !isOpen(item);
  const picked = !done && S.selecting && S.picked.has(item.key);
  const run = agent && item.run ? item.run : null;
  return `<li class="row${nested ? " row-sub" : ""}${done ? " is-done" : ""}${leaving.has(item.key) ? " is-leaving" : ""}${picked ? " is-picked" : ""}${item.pending ? " is-pending" : ""}${run ? " has-run" : ""}"${run ? runAttrs(item) : ""} data-key="${esc(item.key)}">
<div class="under" aria-hidden="true">${allows(item, "done") ? '<span class="u-done">Done</span>' : ""}${allows(item, "snooze") ? '<span class="u-snooze">Snooze</span>' : ""}</div>
<div class="row-fg">${run?.state === "working" ? TRACER : ""}${checkButton(item, agent)}<button class="row-body" data-act="open" data-key="${esc(item.key)}"><span class="row-main"><span class="row-head">${attentionDot(item)}<span class="row-title">${esc(item.title)}</span></span><span class="chips">${chipsFor(item, { list: showList && !nested, nested })}</span>${run ? runLineHTML(item, false) : ""}${statusLine(item)}</span>${whenHTML(item, "chip row-when")}</button>${
    done && allows(item, "reopen")
      ? isDeleted(item)
        ? `<button class="btn btn-small" data-act="restore" data-key="${esc(item.key)}">${ICON.undo}Restore</button>`
        : `<button class="btn btn-small" data-act="reopen" data-key="${esc(item.key)}">${ICON.undo}Reopen</button>`
      : ""
  }</div></li>`;
}

function renderMain() {
  const title = searching() ? "Search" : filtering() ? "Tagged" : viewTitle(S.view);
  setHTML($("#view-title"), esc(title));
  document.title = `${title} · Meat Mode`;
  // Search covers the task views, not Settings or setup.
  $("#search-form").hidden = isPage(S.view);
  const main = $("#main");
  main.dataset.view = S.view.startsWith("list:") ? "list" : S.view;

  // Day load (desktop Today).
  const dl = $("#dayload");
  if (S.view === "today" && S.state) {
    const count = openToday(items(), now());
    const over = count > DAY_CAPACITY;
    setHTML(
      dl,
      `<span class="dl-text">${count} open today</span><div class="meter" role="meter" aria-label="Day load" aria-valuemin="0" aria-valuemax="${DAY_CAPACITY}" aria-valuenow="${Math.min(count, DAY_CAPACITY)}"><div class="meter-fill"></div></div>${
        over ? `<span class="dl-over">More than a day's worth</span>` : ""
      }`,
    );
    dl.classList.toggle("over", over);
    dl.hidden = false;
    $(".meter-fill", dl).style.width = `${Math.min(100, (count / DAY_CAPACITY) * 100)}%`;
  } else {
    dl.hidden = true;
  }

  // The Month / List switch heads Calendar, unless a search or a tag shows in its place.
  const vs = $("#viewswitch");
  const upcoming = (S.view === "upcoming" || S.view === "month") && !searching() && !filtering();
  vs.hidden = !upcoming;
  setHTML(vs, upcoming ? viewSwitchHTML(S.view === "month") : "");

  const list = $("#list");
  const settings = $("#settings");
  const wizard = $("#wizard");
  const banner = $("#setup-banner");
  const bannerBody = bannerHTML(ctx);
  setHTML(banner, bannerBody);
  banner.hidden = !bannerBody;
  // Quick add belongs to the task views, not Settings or setup.
  $("#qa-form").hidden = isPage(S.view);
  // Settings and setup have no task to show, so desktop drops the detail pane.
  $("#app").classList.toggle("page-view", isPage(S.view));
  list.hidden = isPage(S.view);
  settings.hidden = S.view !== "settings";
  wizard.hidden = S.view !== "setup";
  renderTagBar();
  if (S.view === "settings") {
    renderSettings();
    return;
  }
  if (S.view === "setup") {
    setHTML(wizard, wizardHTML(ctx));
    wizardShown();
    return;
  }

  if (!S.state) {
    setHTML(list, `<p class="empty">${S.loadError ? `Could not reach the hub: ${esc(S.loadError)}` : "Loading"}</p>`);
    return;
  }
  if (showingMonth()) {
    setHTML(list, monthViewHTML());
    return;
  }
  const groups = currentGroups();
  // A row names its list only when the rows come from more than one.
  const showList = !S.view.startsWith("list:") && new Set(groups.flatMap((g) => g.items.map(repoKey))).size > 1;
  const agenda = S.view === "today" && !searching() && !filtering();
  if (!groups.length) {
    const none = searching()
      ? "No matches."
      : filtering()
        ? "Nothing has this tag."
        : S.view === "today"
        ? "Nothing due today."
        : S.view === "completed"
          ? "Nothing completed in the last 7 days."
          : S.view === "deleted"
            ? "Nothing deleted in the last 7 days."
            : "Nothing here.";
    setHTML(list, `<p class="empty">${none}</p>`);
    return;
  }
  setHTML(
    list,
    groups
      .map((g) => {
        const extra =
          g.id === "pastdue" && g.items.filter((i) => allows(i, "snooze")).length > 1
            ? `<button class="btn btn-small btn-ghost" data-act="snooze-all">${ICON.moon}Snooze all to tomorrow</button>`
            : "";
        // Today's own groups stand out as a card; Later folds to one line until opened.
        const focus = agenda && FOCUS_GROUPS.includes(g.id) ? " group-focus" : "";
        if (agenda && g.id === "later") {
          const open = S.laterOpen || S.selecting;
          return `<section class="group group-later${focus}" aria-labelledby="g-later"><div class="group-head"><h2 id="g-later"><button class="later-toggle" data-act="later" aria-expanded="${open}"><span class="later-label">${esc(g.label)} <span class="group-count">${g.items.length}</span></span>${open ? "" : laterSummary(g.items)}${ICON.right}</button></h2></div>${
            open ? `<ul class="rows">${g.items.map((i) => rowHTML(i, showList, g.nested?.has(i.key))).join("")}</ul>` : ""
          }</section>`;
        }
        return `<section class="group group-${g.id}${focus}" aria-labelledby="g-${g.id}"><div class="group-head"><h2 id="g-${g.id}">${esc(g.label)} <span class="group-count">${g.items.length}</span></h2>${extra}</div><ul class="rows">${g.items
          .map((i) => rowHTML(i, showList, g.nested?.has(i.key)))
          .join("")}</ul></section>`;
      })
      .join("") + (S.view === "deleted" && !searching() ? '<p class="history-note">Deleted items are removed 7 days after you delete them.</p>' : ""),
  );
}

/** Today's groups that show as a card above the rest. */
const FOCUS_GROUPS: Group["id"][] = ["pastdue", "today", "evening"];

/** Folded Later's line: what in it wants attention, and when the next one is. */
function laterSummary(list: Item[]): string {
  const replies = list.filter((i) => attention(i, user()) === "reply").length;
  const news = list.filter((i) => attention(i, user()) === "new").length;
  const parts: string[] = [];
  if (replies) parts.push(`<span class="later-attn">${replies} new ${replies === 1 ? "reply" : "replies"}</span>`);
  if (news) parts.push(`<span class="later-attn">${news} new</span>`);
  const next = list.map((i) => dateChip(i, now())).find((d) => d);
  if (next) parts.push(`next ${esc(next.text)}`);
  return parts.length ? `<span class="later-sum">${parts.join(" · ")}</span>` : "";
}

// ---------- render: month ----------

/** Whether the month's grid is on screen: the month view, with no search or tag in its place. */
function showingMonth(): boolean {
  return S.view === "month" && S.month !== null && !searching() && !filtering();
}

/** The repeat dates the hub gave for the span `span` ("from/to"), and whether the last load failed. */
const occ = { span: "", list: [] as Occurrence[], failed: false, seq: 0 };

function spanKey(month: Date): string {
  const { from, to } = monthSpan(month);
  return `${from.toISOString()}/${to.toISOString()}`;
}

/** Loads the repeat dates for the grid on screen, then draws it again. A later load wins over an earlier one. */
function loadOccurrences() {
  if (!S.month || S.view !== "month") return;
  const { from, to } = monthSpan(S.month.month);
  const span = spanKey(S.month.month);
  const seq = ++occ.seq;
  if (occ.span !== span) {
    occ.span = span;
    occ.list = [];
    occ.failed = false;
  }
  api.occurrences(from, to).then(
    (r) => {
      if (seq !== occ.seq) return;
      occ.list = r.occurrences ?? [];
      occ.failed = false;
      render();
    },
    () => {
      if (seq !== occ.seq) return;
      occ.list = [];
      occ.failed = true;
      render();
    },
  );
}

/** Whether a month entry can move: see canMove in move.ts. */
function movable(e: MonthEntry): boolean {
  return canMove(e.item, e.kind, allows(e.item, "snooze"));
}

function monthViewHTML(): string {
  const m = S.month!;
  const current = occ.span === spanKey(m.month);
  return monthHTML({
    month: m.month,
    selected: m.selected,
    now: now(),
    entries: monthEntries(items(), current ? occ.list : [], now()),
    occurrencesFailed: current && occ.failed,
    movable,
  });
}

/** Shows `month`, selecting today when it is in it, else its 1st. */
function showMonth(month: Date) {
  const today = startOfDay(now());
  const sameMonth = monthOf(today).getTime() === month.getTime();
  S.month = { month, selected: sameMonth ? today : month };
  render();
  loadOccurrences();
}

function selectDay(day: string) {
  const d = fromYmd(day);
  if (!S.month || !d) return;
  S.month.selected = d;
  render();
  $<HTMLElement>(`#list .mday[data-day="${CSS.escape(day)}"]`)?.focus({ preventScroll: true });
}

/** The month entry an element names with its data-key, data-kind and data-at, with its item. */
function entryOf(el: HTMLElement): { item: Item; entry: MoveEntry } | null {
  const item = byKey(el.dataset.key ?? null);
  const at = new Date(el.dataset.at ?? "");
  const kind = el.dataset.kind as EntryKind | undefined;
  if (!item || !kind || Number.isNaN(at.getTime())) return null;
  return { item, entry: { kind, at } };
}

/**
 * Moves a month entry to `day`, through the action pipeline with an Undo.
 * Returns why the move was refused, or "".
 */
function moveEntry(item: Item, entry: MoveEntry, day: Date): string {
  const d = decideMove(item, entry, day, now(), allows(item, "snooze"), newId());
  if (d.kind === "refuse") return d.text;
  if (d.kind === "move") void closing([d.action], d.toast);
  return "";
}

/** The entry being dragged on desktop. */
let dragged: { item: Item; entry: MoveEntry } | null = null;

// Native drag and drop, which fires for a mouse only: an entry in the grid drops on a day cell.
function setupMonthDrag(list: HTMLElement) {
  const cellOf = (e: DragEvent) => (dragged ? (e.target as HTMLElement).closest<HTMLElement>(".mgrid td[data-day]") : null);
  const unmark = () => {
    for (const td of list.querySelectorAll(".mgrid .is-drop")) td.classList.remove("is-drop");
  };
  list.addEventListener("dragstart", (e) => {
    const el = (e.target as HTMLElement).closest<HTMLElement>(".ment[draggable]");
    dragged = el ? entryOf(el) : null;
    if (!dragged || !e.dataTransfer) return;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", dragged.item.title);
  });
  list.addEventListener("dragover", (e) => {
    const td = cellOf(e);
    if (!td) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
    if (!td.classList.contains("is-drop")) {
      unmark();
      td.classList.add("is-drop");
    }
  });
  list.addEventListener("drop", (e) => {
    const td = cellOf(e);
    const was = dragged;
    dragged = null;
    unmark();
    if (!td || !was) return;
    e.preventDefault();
    const day = fromYmd(td.dataset.day ?? "");
    if (!day) return;
    const why = moveEntry(was.item, was.entry, day);
    if (why) showToast(why);
  });
  list.addEventListener("dragend", () => {
    dragged = null;
    unmark();
  });
}

/** When the touch that opened the Move dialog by a long press lifted: the click its release fires is not a tap. */
let longPressEnded = 0;

// Touch has no native drag: holding a movable entry still opens its Move dialog.
function setupLongPress(list: HTMLElement) {
  let press: { id: number; x: number; y: number; timer: ReturnType<typeof setTimeout> } | null = null;
  /** The pointer whose long press opened the dialog, until it lifts. */
  let held: number | null = null;
  const cancel = () => {
    if (press) clearTimeout(press.timer);
    press = null;
  };
  list.addEventListener("pointerdown", (e) => {
    if (e.pointerType === "mouse") return;
    const el = (e.target as HTMLElement).closest<HTMLElement>("[data-movable]");
    cancel();
    if (!el) return;
    press = {
      id: e.pointerId,
      x: e.clientX,
      y: e.clientY,
      timer: setTimeout(() => {
        held = press?.id ?? null;
        press = null;
        openMove(el);
      }, LONG_PRESS_MS),
    };
  });
  list.addEventListener("pointermove", (e) => {
    if (press && e.pointerId === press.id && Math.hypot(e.clientX - press.x, e.clientY - press.y) > LONG_PRESS_SLOP) cancel();
  });
  // The release may reach the dialog or its scrim, so it is watched on the window.
  window.addEventListener("pointerup", (e) => {
    if (held !== null && e.pointerId === held) longPressEnded = Date.now();
    held = null;
    cancel();
  });
  list.addEventListener("pointercancel", cancel);
  // The browser's own long-press menu would cover the dialog.
  list.addEventListener("contextmenu", (e) => {
    if ((e.target as HTMLElement).closest("[data-movable]")) e.preventDefault();
  });
}

let moveReturn: { key: string; kind: string } | null = null;

/** Opens the Move dialog for the entry `el` names, with its day filled in. */
function openMove(el: HTMLElement) {
  const found = entryOf(el);
  if (!found || !canMove(found.item, found.entry.kind, allows(found.item, "snooze"))) return;
  S.move = { key: found.item.key, kind: found.entry.kind, at: found.entry.at.toISOString(), error: "" };
  moveReturn = { key: found.item.key, kind: found.entry.kind };
  render();
  setValue("#move-date", ymd(found.entry.at));
  $<HTMLInputElement>("#move-date").focus();
}

/** Closes the Move dialog, putting focus back on the entry's Move button when it is still there. */
function closeMove() {
  S.move = null;
  render();
  const r = moveReturn;
  moveReturn = null;
  if (r) document.querySelector<HTMLElement>(`#list [data-act=month-move][data-key="${CSS.escape(r.key)}"][data-kind="${r.kind}"]`)?.focus({ preventScroll: true });
}

function submitMove() {
  const m = S.move;
  const item = byKey(m?.key ?? null);
  if (!m || !item) return closeMove();
  const day = fromYmd($<HTMLInputElement>("#move-date").value);
  const d = day ? decideMove(item, { kind: m.kind, at: new Date(m.at) }, day, now(), allows(item, "snooze"), newId()) : null;
  if (!d || d.kind === "refuse") {
    m.error = d?.kind === "refuse" ? d.text : "Pick a day.";
    render();
    $<HTMLInputElement>("#move-date").focus();
    return;
  }
  closeMove();
  if (d.kind === "move") void closing([d.action], d.toast);
}

function renderMoveDialog() {
  const dlg = $("#move");
  const m = S.move;
  dlg.hidden = !m;
  if (!m) {
    setHTML(dlg, "");
    return;
  }
  const err = m.error ? ' aria-invalid="true" aria-describedby="move-error"' : "";
  setHTML(
    dlg,
    `<div class="dialog-card" role="dialog" aria-modal="true" aria-labelledby="move-h"><div class="pop-head"><h2 id="move-h">Move to another day</h2><button class="icon-btn" data-act="move-close" aria-label="Close">${ICON.close}</button></div><p class="muted small dialog-sub">${esc(byKey(m.key)?.title ?? "")}</p><form class="move-form" data-form="move" novalidate><label for="move-date">Day</label><input id="move-date" type="date" min="${ymd(now())}" required${err}>${
      m.error ? `<p id="move-error" class="row-error" role="alert">${esc(m.error)}</p>` : ""
    }<div class="btn-row"><button type="submit" class="btn btn-primary">Move</button><button type="button" class="btn" data-act="move-close">Cancel</button></div></form></div>`,
  );
}

/** The tag filter's chip, with the way back to the view; hidden with no filter. */
function renderTagBar() {
  const bar = $("#tagbar");
  const on = filtering();
  bar.hidden = !on;
  setHTML(
    bar,
    on
      ? `<span class="tagbar-chip">${ICON.tag}<span>Tag: <b>${esc(S.tag!)}</b></span><button type="button" class="tagbar-clear" data-act="tag-clear" aria-label="${esc(`Clear the tag filter ${S.tag!}`)}" title="Clear">${ICON.close}</button></span>`
      : "",
  );
}

/** Filters the list to the items with `tag`, from every list; null ends the filter. */
function filterTag(tag: string | null) {
  S.tag = tag;
  S.selected = null;
  S.selecting = false;
  S.picked = new Set();
  // The list is behind the phone's detail sheet, so the sheet makes way.
  if (tag !== null && isPhone()) closeDetail();
  render();
  $("#list").scrollTop = 0;
}

function applySelection() {
  for (const li of document.querySelectorAll<HTMLElement>("#list .row")) {
    const key = li.dataset.key!;
    li.classList.toggle("is-selected", key === S.selected);
    li.classList.toggle("is-open", key === S.open);
    if (key === S.selected) li.setAttribute("aria-current", "true");
    else li.removeAttribute("aria-current");
  }
}

/** The Select button and, in select mode, the bulk bar. */
function renderBulk() {
  const btn = $("#select-btn");
  btn.hidden = !selectable();
  btn.setAttribute("aria-pressed", String(S.selecting));
  $("#app").classList.toggle("selecting", S.selecting);
  const bar = $("#bulkbar");
  bar.hidden = !S.selecting;
  if (!S.selecting) {
    setHTML(bar, "");
    return;
  }
  const pickable = pickableKeys(currentGroups());
  const picks = [...S.picked].map(byKey).filter((i): i is Item => Boolean(i));
  // With nothing picked, the label names what Select all would pick.
  const label = bulkDismissLabel(picks.length ? picks : pickable.map(byKey).filter((i): i is Item => Boolean(i)));
  const off = picks.length ? "" : " disabled";
  // Each action shows only when every pick's list can apply it.
  const every = (kind: Capability) => picks.every((i) => allows(i, kind));
  setHTML(
    bar,
    `<span class="bulk-count" aria-live="polite">${picks.length} selected</span><div class="bulk-acts">${
      every("done") ? `<button class="btn btn-small" data-act="bulk-done"${off}>${ICON.check}Done</button>` : ""
    }${every("snooze") ? `<button class="btn btn-small" data-act="bulk-snooze"${off}>${ICON.moon}Snooze…</button>` : ""}${
      every("dismiss") ? `<button class="btn btn-small btn-danger" data-act="bulk-dismiss"${off}>${ICON.trash}${esc(label)}</button>` : ""
    }</div><div class="bulk-more"><button class="btn btn-small btn-ghost" data-act="bulk-all">Select all</button><button class="btn btn-small btn-ghost" data-act="bulk-cancel">Cancel</button></div>`,
  );
}

// ---------- render: detail ----------

/** The snooze choices: the presets as a list, then one field for any other time. */
function snoozeBlock(prefix: string): string {
  return `<ul class="snooze-list" role="list">${snoozeOptions(now())
    .map(
      (o) =>
        `<li><button class="snz" data-act="snooze-opt" data-until="${o.until.toISOString()}"><span class="snz-label">${esc(o.label)}</span><span class="snz-hint">${esc(o.hint)}</span></button></li>`,
    )
    .join("")}</ul>${snoozeTypeForm(prefix)}`;
}

/** Text typed in a snooze time box, by the box's id, so its line survives a redraw. */
const snoozeTyped = new Map<string, string>();

/**
 * One field for a time of the user's own: typed the way quick add reads one,
 * or chosen from the browser's date and time picker behind the calendar button.
 */
function snoozeTypeForm(prefix: string): string {
  const id = `${prefix}-type`;
  const n = now();
  const { until, line } = typedSnooze(snoozeTyped.get(id) ?? "", n);
  const invalid = !until && line ? ` aria-invalid="true"` : "";
  return `<form class="snooze-time" data-form="snooze-type"><label for="${id}">Or type a time</label><div class="snooze-time-row"><div class="snooze-field"><input id="${id}" type="text" autocomplete="off" enterkeyhint="go" placeholder="8pm, in 2 hours, monday" aria-describedby="${id}-now"${invalid} data-type-time><button type="button" class="snooze-cal" data-act="snooze-cal" aria-label="Choose a date and time">${ICON.upcoming}</button><input id="${id}-pick" class="snooze-native" type="datetime-local" min="${toLocalInput(n)}" tabindex="-1" aria-hidden="true" data-pick></div><button type="submit" class="btn btn-primary"${until ? "" : " disabled"}>Snooze</button></div><p id="${id}-now" class="snooze-type-now" aria-live="polite">${esc(line)}</p></form>`;
}

/** Updates the line under a snooze time box and its Snooze button, and only those, while typing. */
function updateSnoozeType(input: HTMLInputElement) {
  snoozeTyped.set(input.id, input.value);
  const { until, line } = typedSnooze(input.value, now());
  const out = document.getElementById(`${input.id}-now`);
  if (out) out.textContent = line;
  if (!until && line) input.setAttribute("aria-invalid", "true");
  else input.removeAttribute("aria-invalid");
  const submit = input.form?.querySelector<HTMLButtonElement>("button[type=submit]");
  if (submit) submit.disabled = !until;
}

/** Opens the browser's date and time picker for a snooze time box. */
function openSnoozePicker(btn: HTMLElement) {
  const pick = btn.parentElement!.querySelector<HTMLInputElement>("input[data-pick]")!;
  const typed = typedSnooze(btn.parentElement!.querySelector<HTMLInputElement>("input[data-type-time]")!.value, now()).until;
  pick.min = toLocalInput(now());
  if (typed) pick.value = toLocalInput(typed);
  try {
    pick.showPicker();
  } catch {
    // No showPicker, or the browser refused it: focusing the field opens the picker on phones.
    pick.focus();
  }
}

/** A time chosen in the browser's picker fills the snooze time box with it. */
function snoozePicked(pick: HTMLInputElement) {
  const input = pick.parentElement!.querySelector<HTMLInputElement>("input[data-type-time]")!;
  const d = new Date(pick.value);
  if (!pick.value || isNaN(d.getTime())) return;
  input.value = fmtSnoozeTime(d, now());
  updateSnoozeType(input);
}

function submitSnoozeType(form: HTMLFormElement) {
  const input = form.querySelector<HTMLInputElement>("input[data-type-time]")!;
  updateSnoozeType(input);
  const { until } = typedSnooze(input.value, now());
  if (!until) {
    input.setAttribute("aria-invalid", "true");
    const out = document.getElementById(`${input.id}-now`);
    if (out && !input.value.trim()) out.textContent = "Type a time, such as 8pm or tomorrow morning.";
    input.focus();
    return;
  }
  const keys = form.closest("#snooze") ? S.snoozeKeys ?? [] : S.open ? [S.open] : [];
  snoozeTyped.delete(input.id);
  input.value = "";
  doSnooze(keys, until);
  if (!form.closest("#snooze")) leaveSnoozePage();
}

/** The title of the sheet: a button that turns into a field on a personal item, text on an agent's. */
function titleHTML(item: Item, editable: boolean): string {
  if (!editable) return `<h2 id="d-title" class="d-title">${esc(item.title)}</h2>`;
  if (S.inline?.key === item.key && S.inline.field === "title") {
    return `<h2 id="d-title" class="d-title"><form class="inline-form" data-form="inline"><input id="d-title-input" class="d-title-input" type="text" maxlength="${TITLE_MAX}" autocomplete="off" enterkeyhint="done" aria-label="Title"${S.inline.error ? ' aria-invalid="true" aria-describedby="d-inline-error"' : ""}></form></h2>`;
  }
  return `<h2 id="d-title" class="d-title"><button class="d-title-btn" data-act="inline" data-field="title" title="Rename">${esc(item.title)}</button></h2>`;
}

/** The inline field's error, under it. */
function inlineErrorHTML(item: Item, field: Inline["field"]): string {
  const e = S.inline;
  return e?.key === item.key && e.field === field && e.error ? `<p id="d-inline-error" class="row-error" role="alert">${esc(e.error)}</p>` : "";
}

/** The schedule card: when it is due and how it repeats, in plain words. It opens the Schedule page. */
function scheduleCardHTML(item: Item): string {
  const n = now();
  const card = scheduleCard(item, n);
  const overdue = item.dueAt && isOpen(item) && new Date(item.dueAt) < n ? " is-overdue" : "";
  const snoozed = item.deferUntil && new Date(item.deferUntil) > n ? `<span class="sched-line sched-snz">${ICON.moon}Snoozed until ${esc(fmtWhenAt(new Date(item.deferUntil), n))}</span>` : "";
  const repeat = card.repeat ? `<span class="sched-line">${item.repeat ? ICON.repeat : ""}${esc(card.repeat)}</span>` : "";
  return `<button class="sched-card" id="d-sched" data-act="schedule" data-key="${esc(item.key)}"><span class="sr">Schedule: </span><span class="sched-when${overdue}">${esc(card.when)}</span>${repeat}${snoozed}<span class="sched-chev" aria-hidden="true">${ICON.right}</span></button>`;
}

/**
 * The rows under the schedule card, label on the left and value on the
 * right. Each change saves on the spot. The list shows where the task lives;
 * there is no move between lists.
 */
function propsHTML(item: Item): string {
  const r = repos().find((x) => repoKey(x) === repoKey(item));
  const colour = repoColours(repos()).get(repoKey(item)) ?? 0;
  const list = `<div class="prop"><span class="prop-label">List</span><span class="prop-val"><span class="dot c${colour}" aria-hidden="true"></span>${esc(r ? repoLabel(r) : item.repo)}</span></div>`;
  const lead = `<div class="prop"><label class="prop-label" for="p-lead">Remind early</label>${leadSelect("p-lead", item.remindBefore ?? null, true, ' class="prop-select"')}</div>`;
  const prio = `<div class="prop"><label class="prop-label" for="p-prio">Priority</label><select id="p-prio" class="prop-select">${priorityOptions(item.priority)
    .map((o) => `<option value="${o.value}"${o.value === item.priority ? " selected" : ""}>${esc(o.label)}</option>`)
    .join("")}</select></div>`;
  return `<div class="props">${list}${lead}${tagsRowHTML(item)}${prio}${parentRowHTML(item)}</div>`;
}

/** The Tags row: each tag filters the list by it, and Add or Change turns the row into a field. */
function tagsRowHTML(item: Item): string {
  const tags = shownTags(item);
  if (S.inline?.key === item.key && S.inline.field === "tags") {
    return `<form class="prop prop-edit inline-form" data-form="inline"><label class="prop-label" for="p-tags">Tags</label><input id="p-tags" type="text" autocomplete="off" autocapitalize="none" spellcheck="false" enterkeyhint="done" placeholder="garden errands" aria-describedby="p-tags-hint${S.inline.error ? " d-inline-error" : ""}"${S.inline.error ? ' aria-invalid="true"' : ""}></form><p class="muted small prop-hint" id="p-tags-hint">One word each, separated by spaces.</p>${inlineErrorHTML(item, "tags")}`;
  }
  const chips = tags
    .map((t) => `<button type="button" class="chip chip-tag" data-act="tag" data-tag="${esc(t)}" aria-label="${esc(`Show items tagged ${t}`)}">${ICON.tag}${esc(t)}</button>`)
    .join("");
  return `<div class="prop"><span class="prop-label">Tags</span><span class="prop-val prop-tags">${chips}<button class="link-btn prop-link" data-act="inline" data-field="tags" aria-label="${tags.length ? "Change tags" : "Add tags"}">${tags.length ? "Change" : "Add"}</button></span></div>`;
}

/**
 * The "Sub-task of" row: the top level, or an item of the same list to move
 * it under. Left out when there is nowhere to move it.
 */
function parentRowHTML(item: Item): string {
  const opts = moveTargets(item, items()).map((t) => [t.id, t.title]);
  if (item.parent && !opts.some(([id]) => id === item.parent)) opts.unshift([item.parent, parentTitle(item, items()) || item.parent]);
  if (!opts.length) return "";
  const cur = item.parent ?? "";
  const pick = [["", "None"], ...opts].map(([v, label]) => `<option value="${esc(v)}"${v === cur ? " selected" : ""}>${esc(label)}</option>`).join("");
  return `<div class="prop"><label class="prop-label" for="p-parent">Sub-task of</label><select id="p-parent" class="prop-select">${pick}</select></div>`;
}

/** Details: Markdown to read, and a field that saves when it loses focus. */
function detailsHTML(item: Item): string {
  const head = '<h3 id="d-desc-h">Details</h3>';
  if (S.inline?.key === item.key && S.inline.field === "description") {
    return `<section class="d-section" aria-labelledby="d-desc-h">${head}<textarea id="d-desc" rows="4" maxlength="${TEXT_MAX}" aria-labelledby="d-desc-h" aria-describedby="d-desc-hint"></textarea><p class="muted small" id="d-desc-hint">Saves when you leave the field. Markdown works.</p>${inlineErrorHTML(item, "description")}</section>`;
  }
  if (!item.description) return `<button class="add-details" data-act="inline" data-field="description">${ICON.edit}Add details</button>`;
  return `<section class="d-section" aria-labelledby="d-desc-h"><div class="d-sec-head">${head}<button class="link-btn" data-act="inline" data-field="description" aria-label="Edit details" title="Edit details">${ICON.edit}</button></div><div class="md d-desc">${renderMarkdown(item.description)}</div></section>`;
}

/**
 * The bar pinned to the bottom of the sheet: the main action, Snooze, and
 * More for the rest. A closed item's main action brings it back.
 */
function actionBarHTML(item: Item): string {
  const key = esc(item.key);
  const agent = isAgentItem(item);
  const open = isOpen(item);
  let main = "";
  if (open && allows(item, "done")) main = `<button class="btn btn-primary" data-act="done" data-key="${key}">${ICON.check}Done</button>`;
  else if (!open && allows(item, "reopen")) {
    main = isDeleted(item)
      ? `<button class="btn btn-primary" data-act="restore" data-key="${key}">${ICON.undo}Restore</button>`
      : `<button class="btn btn-primary" data-act="reopen" data-key="${key}">${ICON.undo}Reopen</button>`;
  }
  const snooze = open && allows(item, "snooze") ? `<button class="btn" id="d-snooze" data-act="snooze-page" data-key="${key}">${ICON.moon}Snooze</button>` : "";
  const more = [
    open && allows(item, "dismiss")
      ? `<button class="more-item${agent ? "" : " more-danger"}" data-act="dismiss" data-key="${key}">${agent ? `${ICON.ban}Won't do` : `${ICON.trash}Delete`}</button>`
      : "",
    `<button class="more-item" data-act="copy-id" data-key="${key}">Copy id <code>${esc(item.id)}</code></button>`,
  ].join("");
  const shown = S.moreOpen && S.open === item.key;
  return `<div class="d-bar">${main}${snooze}<div class="more-wrap"><button class="btn btn-icon" id="d-more" data-act="more" aria-label="More actions" title="More actions" aria-haspopup="true" aria-expanded="${shown}" aria-controls="d-more-menu">${ICON.more}</button><div id="d-more-menu" class="more-menu"${shown ? "" : " hidden"}>${more}</div></div></div>`;
}

/** A page inside the sheet: its head with the way back, its title and an optional action. */
function pageHeadHTML(title: string, action = ""): string {
  return `<div class="grip" aria-hidden="true"></div><div class="page-head"><button class="link-btn page-back" data-act="page-back" aria-label="Back to the task">${ICON.left}Task</button><h2 id="pg-h" class="page-title" tabindex="-1">${esc(title)}</h2><span class="page-act">${action}</span></div>`;
}

function snoozePageHTML(item: Item): string {
  return `<div class="detail-inner sheet-page" role="region" aria-labelledby="pg-h">${pageHeadHTML("Snooze")}<p class="muted small page-sub">${esc(item.title)}</p>${snoozeBlock("d")}</div>`;
}

const REPEAT_SEGMENTS: [EditDraft["repeat"], string][] = [
  ["", "Never"],
  ["day", "Day"],
  ["week", "Week"],
  ["month", "Month"],
  ["year", "Year"],
];
/** Monday first, as people list a week. */
const WEEK: [number, string][] = [
  [1, "Mon"],
  [2, "Tue"],
  [3, "Wed"],
  [4, "Thu"],
  [5, "Fri"],
  [6, "Sat"],
  [0, "Sun"],
];

/** "days" for every 2, "day" for every 1. */
function unitText(d: EditDraft): string {
  return d.repeat ? `${d.repeat}${d.interval.trim() === "1" ? "" : "s"}` : "";
}

/** A row of radio buttons drawn as one segmented control. */
function segmentsHTML(name: string, legend: string, choices: [string, string][], value: string, describedBy = ""): string {
  return `<fieldset class="seg"${describedBy ? ` aria-describedby="${describedBy}"` : ""}><legend>${esc(legend)}</legend><div class="seg-row">${choices
    .map(([v, label]) => `<label class="seg-opt"><input type="radio" name="${name}" value="${esc(v)}"${v === value ? " checked" : ""}><span>${esc(label)}</span></label>`)
    .join("")}</div></fieldset>`;
}

/**
 * The Schedule page: a typed field read with quick add's parser and a
 * readout of what the draft will save, then the same schedule as controls.
 * Both edit one draft. Moving a repeating task asks first whether only this
 * one moves.
 */
function schedulePageHTML(item: Item, p: SchedulePage): string {
  const n = now();
  const d = p.draft;
  const readout = p.typedError || scheduleReadout(d, n);
  const bad = p.typedError ? " readout-bad" : "";
  let repeat = "";
  let ends = "";
  if (d.repeat) {
    const days =
      d.repeat === "week"
        ? `<fieldset class="days" aria-describedby="sched-days-hint"${d.fromDone ? " disabled" : ""}><legend>On</legend><div class="days-row">${WEEK.map(
            ([wd, name]) => `<label class="day-chip"><input type="checkbox" data-weekday="${wd}"${d.weekdays.includes(wd) ? " checked" : ""}><span>${name}</span></label>`,
          ).join("")}</div></fieldset><p class="muted small" id="sched-days-hint">${
            d.fromDone ? "Days need The schedule: When I finish counts from the day you finish it." : "With none picked, it repeats on the date's day."
          }</p>`
        : "";
    repeat = `<div class="props"><div class="prop"><label class="prop-label" for="sched-interval">Every</label><span class="prop-ctl"><input id="sched-interval" type="number" min="1" max="${MAX_REPEAT_INTERVAL}" step="1" inputmode="numeric"><span id="sched-unit">${unitText(d)}</span></span></div></div>${days}${segmentsHTML(
      "sched-from",
      "Count the next one from",
      [
        ["schedule", "The schedule"],
        ["done", "When I finish"],
      ],
      d.fromDone ? "done" : "schedule",
      "sched-from-hint",
    )}<p class="muted small" id="sched-from-hint">${esc(countFromHint(d))}</p>`;
    ends = `<div class="prop"><label class="prop-label" for="sched-until">Ends</label><span class="prop-ctl"><input id="sched-until" type="date"${d.date ? ` min="${esc(d.date)}"` : ""}><button type="button" class="link-btn" data-act="sched-clear-until">No end date</button></span></div>`;
  }
  const ask = p.ask
    ? `<div class="sched-ask" role="group" aria-labelledby="ask-h"><p id="ask-h" class="ask-q" tabindex="-1">This task repeats. Move only this one, or the whole schedule?</p><button type="button" class="btn btn-primary" data-act="sched-once" aria-describedby="ask-once">Just this one</button><p class="muted small" id="ask-once">Snoozes it to ${esc(fmtWhenAt(p.ask.until, n))}. The repeat stays as it is.</p><button type="button" class="btn" data-act="sched-all" aria-describedby="ask-all">From now on</button><p class="muted small" id="ask-all">Moves the schedule to count on from ${esc(fmtWhenAt(p.ask.until, n))}.</p><button type="button" class="link-btn" data-act="sched-ask-cancel">Cancel</button></div>`
    : "";
  return `<div class="detail-inner sheet-page" role="region" aria-labelledby="pg-h">${pageHeadHTML("Schedule", `<button type="submit" form="sched-form" class="btn btn-primary btn-small">Save</button>`)}
<p class="muted small page-sub">${esc(item.title)}</p>
${ask}
<form id="sched-form" class="sched-form" data-form="schedule" novalidate>
<label class="sched-typed-label" for="sched-typed">Type when</label><input id="sched-typed" type="text" autocomplete="off" autocapitalize="none" spellcheck="false" enterkeyhint="done" placeholder="every sunday 12pm" aria-describedby="sched-readout"${p.typedError ? ' aria-invalid="true"' : ""}>
<p id="sched-readout" class="readout${bad}" aria-live="polite">${esc(readout)}</p>
<h3 class="sched-or">Or set it here</h3>
<div class="props">
<div class="prop"><label class="prop-label" for="sched-date">Date</label><span class="prop-ctl"><input id="sched-date" type="date"><button type="button" class="link-btn" data-act="sched-clear-date"${d.date ? "" : " hidden"}>Clear</button></span></div>
<div class="prop"><label class="prop-label" for="sched-time">Time</label><span class="prop-ctl"><input id="sched-time" type="time"></span></div>
</div>
${segmentsHTML("sched-repeat", "Repeat", REPEAT_SEGMENTS, d.repeat)}
${repeat}
<div class="props">${ends}<div class="prop"><label class="prop-label" for="sched-lead">Remind early</label>${leadSelect("sched-lead", d.remindBefore, true, ' class="prop-select"')}</div></div>
${p.error ? `<p class="row-error" role="alert">${esc(p.error)}</p>` : ""}
</form></div>`;
}

/**
 * The sheet's sub-tasks: a sub-task names its parent, and a top-level item
 * lists its sub-tasks with a field to add one. Sub-tasks go one level deep.
 */
function subtasksHTML(item: Item): string {
  if (item.parent) {
    const p = parentOf(item, items());
    const title = parentTitle(item, items());
    if (!title) return "";
    const name = p ? `<button class="link-btn" data-act="open" data-key="${esc(p.key)}">${esc(title)}</button>` : esc(title);
    return `<p class="d-parent">${ICON.subtask}Part of ${name}</p>`;
  }
  const subs = subtasksOf(item, items());
  const canAdd = isOpen(item) && !item.id.startsWith("pending-") && allows(item, "subtask");
  if (!subs.length && !canAdd) return "";
  const rows = subs
    .map((c) => {
      const agent = isAgentItem(c);
      const remove = canRemoveSubtask(c)
        ? `<button class="link-btn sub-remove" data-act="sub-remove" data-key="${esc(c.key)}" aria-label="${esc(`Remove: ${c.title}`)}" title="${agent ? "Won't do" : "Delete"}">${ICON.close}</button>`
        : "";
      return `<li class="sub${isOpen(c) ? "" : " sub-done"}">${checkButton(c, agent)}<button class="sub-title" data-act="open" data-key="${esc(c.key)}">${esc(c.title)}</button>${remove}</li>`;
    })
    .join("");
  const count = subtaskChip(item, items());
  const add = canAdd
    ? `<form class="pick-row" data-form="sub-add"><label class="sr" for="sub-input">Add a sub-task</label><input id="sub-input" type="text" maxlength="${TITLE_MAX}" autocomplete="off" enterkeyhint="done" placeholder="Add a sub-task"><button type="submit" class="btn">${ICON.plus}Add</button></form>`
    : "";
  return `<section class="d-section" aria-labelledby="d-subs"><h3 id="d-subs">Sub-tasks${count ? ` <span class="group-count">${count}</span>` : ""}</h3>${
    rows ? `<ul class="subtasks">${rows}</ul>` : ""
  }${add}</section>`;
}

/**
 * An agent item's card in the sheet: why the agent asks, the run's line in
 * its state's colour, and the one button that fits the state. Ask agent and
 * Retry wake the agent; Stop ends a live run. Where agents are off for the
 * list, the card says so and gives the command that switches them on at the
 * list's machine, since the app cannot.
 */
function runCardHTML(item: Item, allowed: boolean, askable: boolean): string {
  const who = agentName(item);
  const run = item.run;
  const key = esc(item.key);
  const label = item.description ? `${esc(who)} asks because` : esc(who);
  const reason = item.description ? `<div class="md">${renderMarkdown(item.description)}</div>` : "";
  let line = runLineHTML(item, true);
  let button = "";
  if (run?.state === "working") {
    button = `<button class="btn btn-small btn-danger btn-icon" data-act="stop" data-key="${key}" aria-label="${esc(`Stop ${who}`)}" title="${esc(`Stop ${who}`)}">${ICON.stop}</button>`;
  } else if (allowed && run?.state === "stopped") {
    button = `<button class="btn btn-small btn-agent" data-act="ask" data-key="${key}">${ICON.spark}Retry</button>`;
  } else if (allowed && !run) {
    line = '<span class="run-line run-idle">The agent reads your latest notes, then acts or replies.</span>';
    button = `<button class="btn btn-small btn-agent" data-act="ask" data-key="${key}">${ICON.spark}Ask agent</button>`;
  }
  // A tracker without agents has no switch to point at.
  const off = allowed || !askable
    ? ""
    : `<div class="run-off"><p>Agents are off for this list. <button class="link-btn" data-act="agents-settings">Open Settings</button></p>${agentsOnHTML(item.bridge, item.repo, "run-agents-cmd")}</div>`;
  const live = line || button ? `<div class="run-live">${line}${button}</div>` : "";
  return `<div class="why why-agent run-card"${runAttrs(item)}>${run?.state === "working" ? TRACER : ""}<div class="why-label">${label}</div>${reason}${live}${off}</div>`;
}

/**
 * The note box's buttons. Where an agent may work, a quiet icon-only save
 * sits beside one pink button that also wakes the agent: Save and ask agent,
 * Send to <agent> while it works, Reply to <agent> while it waits.
 */
function composeHTML(item: Item, allowed: boolean): string {
  const key = esc(item.key);
  if (!allowed) return `<button class="btn" data-act="note" data-key="${key}">${ICON.note}Save note</button>`;
  const ask = askLabel(item);
  return `<div class="compose-row"><button class="btn btn-icon" data-act="note" data-key="${key}" aria-label="Save note" title="Save the note without asking the agent">${ICON.note}</button><button class="btn btn-agent" data-act="ask-note" data-key="${key}">${ask.kind === "reply" ? ICON.reply : ICON.spark}${esc(ask.label)}</button></div>`;
}

/**
 * The note box under the notes. A list whose tracker takes no notes has
 * none; where it still runs agents, the run card's Ask agent wakes them.
 */
function noteBoxHTML(item: Item, allowed: boolean): string {
  if (!allows(item, "note")) return "";
  return `<label for="note-input" class="note-label">Add a note</label><textarea id="note-input" rows="3" maxlength="${TEXT_MAX}"></textarea>${composeHTML(item, allowed)}`;
}

function noteHTML(item: Item, c: Comment, editable: boolean, queued = false): string {
  const own = appAuthor(user());
  const mine = c.author === own;
  const unread = isUnreadComment(c.createdAt, c.author, S.openSeenAt, user());
  const editing = editable && S.noteEdit?.key === item.key && S.noteEdit.commentId === c.id;
  const body = editing
    ? `<form class="note-edit" data-form="note-edit"><label class="sr" for="note-edit-input">Edit note</label><textarea id="note-edit-input" rows="3" maxlength="${TEXT_MAX}"></textarea><div class="btn-row"><button type="submit" class="btn btn-small btn-primary">Save</button><button type="button" class="btn btn-small" data-act="note-edit-cancel">Cancel</button></div></form>`
    : `<div class="md">${renderMarkdown(c.text)}</div>`;
  const tools =
    editable && !editing
      ? `<span class="note-tools"><button class="link-btn" id="ne-${esc(c.id)}" data-act="note-edit" data-id="${esc(c.id)}" aria-label="Edit note" title="Edit note">${ICON.edit}</button><button class="link-btn" data-act="note-delete" data-id="${esc(c.id)}" aria-label="Delete note" title="Delete note">${ICON.trash}</button></span>`
      : "";
  // A follow-up the live run has not read yet carries the running light until it does.
  const flying = queued ? `<span class="run-line" data-run="working">On its way to ${esc(agentName(item))}</span>` : "";
  return `<li class="note ${mine ? "note-own" : "note-agent"}${unread ? " note-unread" : ""}"${queued ? ' data-run="working"' : ""}>${queued ? TRACER : ""}<div class="note-head"><span class="note-author">${esc(mine ? "You" : c.author)}</span><span class="note-time">${esc(fmtWhen(new Date(c.createdAt), now()))}</span>${unread ? '<span class="chip chip-new">New</span>' : ""}${flying}${tools}</div>${body}</li>`;
}

function renderDetail() {
  const el = $("#detail");
  const item = byKey(S.open);
  const app = $("#app");
  app.classList.toggle("detail-open", !!item);
  if (!item) {
    setHTML(el, '<div class="detail-empty"><p>Select a task to see its details.</p></div>');
    return;
  }
  if (S.page && S.page.key !== item.key) S.page = null;
  const p = S.page;
  if (p?.kind === "snooze") {
    setHTML(el, snoozePageHTML(item));
    return;
  }
  if (p?.kind === "schedule") {
    setHTML(el, schedulePageHTML(item, p));
    // Values never live in the HTML: the draft sets them, where they differ.
    setValue("#sched-typed", p.typed);
    setValue("#sched-date", p.draft.date);
    setValue("#sched-time", p.draft.time);
    setValue("#sched-interval", p.draft.interval);
    setValue("#sched-until", p.draft.until);
    return;
  }
  const agent = isAgentItem(item);
  const editable = canEdit(item);
  const allowed = agent && agentsAllowed(item, repos());
  const why = agent
    ? runCardHTML(item, allowed, allows(item, "ask"))
    : item.description
      ? `<div class="why"><div class="why-label">Details</div><div class="md">${renderMarkdown(item.description)}</div></div>`
      : "";
  const queued = queuedComments(item, user());
  const comments = (item.comments ?? []).map((c) => noteHTML(item, c, editable, queued.has(c.id))).join("");
  const att = attention(item, user());
  const chips = editable
    ? att ? `<div class="chips d-chips"><span class="chip chip-new">${ATTENTION_TEXT[att]}</span></div>` : ""
    : `<div class="chips d-chips">${chipsFor(item, { list: true, detail: true })}<code class="bead-id">${esc(item.id)}</code></div>`;
  const body = editable ? `${scheduleCardHTML(item)}${propsHTML(item)}${detailsHTML(item)}` : why;
  const html = `<div class="detail-inner" role="region" aria-labelledby="d-title">
<div class="grip" aria-hidden="true"></div>
<div class="d-head">${checkButton(item, agent)}${titleHTML(item, editable)}<button class="icon-btn" data-act="close-detail" aria-label="Close details">${ICON.close}</button></div>
${inlineErrorHTML(item, "title")}
${chips}
${statusLine(item) ? `<p class="d-status">${statusLine(item)}</p>` : ""}
${body}
${subtasksHTML(item)}
${attachmentsHTML(item, editable)}
<section class="d-section" aria-labelledby="d-notes"><h3 id="d-notes">Notes and replies</h3>${comments ? `<ol class="notes">${comments}</ol>` : '<p class="muted small">No notes yet.</p>'}
${noteBoxHTML(item, allowed)}</section>
${actionBarHTML(item)}
</div>`;
  if (setHTML(el, html)) {
    const ta = el.querySelector<HTMLTextAreaElement>("#note-input");
    if (ta) ta.value = S.drafts.get(item.key) ?? "";
    const sub = el.querySelector<HTMLInputElement>("#sub-input");
    if (sub) sub.value = S.subDrafts.get(item.key) ?? "";
    if (S.noteEdit?.key === item.key) setValue("#note-edit-input", S.noteEdit.text);
  }
  if (S.inline?.key === item.key) setValue(INLINE_FIELD[S.inline.field], S.inline.text);
}

/** Sets a field's value only when it differs, so a rewrite keeps the caret where it was. */
function setValue(sel: string, value: string) {
  const f = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(sel);
  if (f && f.value !== value) f.value = value;
}

// ---------- render: overlays ----------

function renderSnoozeDialog() {
  const dlg = $("#snooze");
  const keys = S.snoozeKeys;
  dlg.hidden = !keys;
  if (!keys) {
    setHTML(dlg, "");
    return;
  }
  const item = byKey(keys[0]);
  const title = keys.length > 1 ? `${keys.length} tasks` : item?.title ?? "";
  setHTML(
    dlg,
    `<div class="dialog-card" role="dialog" aria-modal="true" aria-labelledby="snz-h"><div class="pop-head"><h2 id="snz-h">Snooze</h2><button class="icon-btn" data-act="snooze-close" aria-label="Close">${ICON.close}</button></div><p class="muted small dialog-sub">${esc(title)}</p>${snoozeBlock("dlg")}</div>`,
  );
}

function renderToast() {
  if (!shellUp) return;
  const t = S.toast;
  setHTML($("#toast"), t ? `<div class="toast"><span>${esc(t.text)}</span>${t.undo ? '<button class="btn btn-small toast-undo" data-act="undo">Undo</button>' : ""}${tellHTML(t)}</div>` : "");
}

/** The undo bar's agent button: Tell <agent>, or a Stop square while the run it started works. */
function tellHTML(t: Toast): string {
  const it = t.tell ? byKey(t.tell) : undefined;
  if (!it) return "";
  const who = agentName(it);
  const key = esc(it.key);
  if (isWorking(it))
    return `<button class="btn btn-small btn-danger btn-icon" data-act="stop" data-key="${key}" aria-label="${esc(`Stop ${who}`)}" title="${esc(`Stop ${who}`)}">${ICON.stop}</button>`;
  return `<button class="btn btn-small btn-agent" data-act="tell" data-key="${key}">${ICON.spark}${esc(`Tell ${who}`)}</button>`;
}

function renderTheme() {
  const theme = getTheme();
  const next = theme === "dark" ? "light" : "dark";
  for (const b of document.querySelectorAll<HTMLElement>("[data-act=theme]")) {
    b.setAttribute("aria-pressed", String(theme === "light"));
    setHTML(b, `${theme === "dark" ? ICON.sun : ICON.moon}<span>${next === "light" ? "Light theme" : "Dark theme"}</span>`);
  }
}

// ---------- render: settings ----------

function renderSettings() {
  // A public DNS record for the hub's name heads Settings, above every section.
  loadStatusIfStale(ctx);
  const warn = publicDnsWarningHTML();
  const sections = sectionsHTML(ctx);
  setHTML($("#settings"), warn ? `${warn}\n${sections}` : sections);
  renderTheme();
}

async function switchSync(bridge: string, repo: string) {
  const r = repos().find((x) => x.bridge === bridge && x.name === repo);
  if (!r) return;
  await post({ id: newId(), kind: "sync", bridge, repo, payload: { on: !r.optedIn } });
  await refresh();
}

// ---------- setup wizard and Settings sections ----------

/** What the setup wizard's steps and the Settings sections in their own modules see of the app. */
const ctx: AppContext = {
  get state() {
    return S.state;
  },
  get session() {
    return S.auth;
  },
  get setup() {
    return setupInfo();
  },
  get view() {
    return S.view;
  },
  now,
  render,
  refresh: async () => {
    // The session too: claiming the hub turns its login on and logs this browser in.
    const sess = api.session().then(
      (s) => void (S.auth = s),
      () => {},
    );
    await Promise.all([refresh(), reloadSetup(), sess]);
    render();
  },
  perform: (acts) => actions.perform(acts),
  toast: (text, undo) => showToast(text, undo),
  setView: (v) => setView(v as View),
  openSettings(section) {
    setView("settings");
    // Each heading has tabindex -1, so it keeps focus when a section that
    // loads its own data draws Settings again.
    document.getElementById(section ?? "")?.focus();
  },
};

// ---------- render ----------

function render() {
  if (!shellUp) return;
  keepSelect();
  renderNav();
  renderSync();
  renderMain();
  renderDetail();
  renderSnoozeDialog();
  renderMoveDialog();
  renderToast();
  renderTheme();
  applySelection();
  renderBulk();
  renderMenu();
  $("#scrim").hidden = !(S.snoozeKeys || S.move || (isPhone() && (S.open || S.qaOpen || S.menuOpen)));
  $("#app").classList.toggle("qa-open", S.qaOpen);
}

// ---------- data ----------

let refreshing: Promise<void> | null = null;
let refreshAgain = false;

async function refresh(): Promise<void> {
  if (!shellUp) return;
  if (refreshing) {
    refreshAgain = true;
    return refreshing;
  }
  refreshing = (async () => {
    try {
      const st = await api.state();
      noteRunEnds(st.items ?? []);
      S.state = st;
      S.loadError = "";
      const server = new Date(st.serverTime).getTime();
      if (!Number.isNaN(server)) S.offset = server - Date.now();
      if (S.selected && !byKey(S.selected)) S.selected = null;
      if (S.syncOpen) void syncActions.reload(renderSync);
    } catch (e) {
      S.loadError = errorText(e);
    }
    if (!shellUp) return;
    render();
    updateQuickAdd();
    // A change to the state may change the repeat dates.
    loadOccurrences();
  })();
  try {
    await refreshing;
  } finally {
    refreshing = null;
  }
  if (refreshAgain) {
    refreshAgain = false;
    await refresh();
  }
}

/** The hub's recent actions, which the sync pill's popup lists while it is open. */
const syncActions = remote(() => api.actions(30));

let es: EventSource | null = null;
let esRetry = 0;
let esTimer: ReturnType<typeof setTimeout> | undefined;
function connectEvents() {
  if (typeof EventSource === "undefined" || !shellUp) return;
  clearTimeout(esTimer);
  es?.close();
  es = new EventSource("/api/events");
  es.addEventListener("open", () => {
    if (esRetry > 0) void refresh();
    esRetry = 0;
  });
  es.addEventListener("change", () => void refresh());
  es.addEventListener("error", () => {
    if (es && es.readyState === EventSource.CLOSED) {
      // A closed stream may mean the session ended: ask once before trying
      // again, so a logged-out app does not keep knocking on /api/events.
      const wait = Math.min(30000, 1000 * 2 ** esRetry++);
      void api.session().then(
        (sess) => {
          if (!showGate(sess)) esTimer = setTimeout(connectEvents, wait);
        },
        (e) => {
          if (!(e instanceof ApiError && e.status === 401)) esTimer = setTimeout(connectEvents, wait);
        },
      );
    }
  });
}

// ---------- actions ----------

const actions = createActions({
  api,
  newId,
  now,
  item: (key) => byKey(key),
  onError: (during, e) => showToast(`Could not ${during}: ${e.message}`),
});

/** Posts one action; the hub's copy of it, or null when it failed (a toast says why). */
async function post(a: NewAction): Promise<Action | null> {
  return (await actions.perform([a])).posted[0]?.applied ?? null;
}

/** The toast's Undo for what `h` posted: reverses it, then loads the state again. */
function undoing(h: Handle): (() => Promise<void>) | undefined {
  // Once applied, a close undoes by a reopen, so a list that cannot reopen gets no Undo.
  const stuck = h.posted.some(({ sent }) => {
    const back = inverse(sent, "");
    const it = byKey(sent.key ?? null);
    return back && it && !allows(it, back.kind as Capability);
  });
  if (stuck) return undefined;
  return async () => {
    await actions.undo(h, { onPatched: render });
    await refresh();
  };
}

/** Performs closing actions, drawing their local change with `draw`, and offers Undo. */
async function closing(acts: NewAction[], text: string, opts: { tell?: string; draw?: () => void } = {}) {
  const h = await actions.perform(acts, { onPatched: opts.draw ?? render });
  if (h.posted.length) showToast(text, undoing(h), opts.tell);
  await refresh();
}

function moveSelectionOff(keys: string[]) {
  if (!S.selected || !keys.includes(S.selected)) return;
  const order = visibleKeys().filter((k) => {
    const it = byKey(k);
    return it && isOpen(it);
  });
  const idx = order.indexOf(S.selected);
  const rest = order.filter((k) => !keys.includes(k));
  S.selected = rest.find((k) => order.indexOf(k) > idx) ?? rest[rest.length - 1] ?? null;
}

/**
 * Done on an item. A repeating personal item moves to its next date and stays
 * open; its toast names that date once the state has it.
 */
async function doDone(key: string) {
  const it = byKey(key);
  if (!it || !isOpen(it) || !allows(it, "done")) return;
  const blocked = blockedBySubtasks(it);
  if (blocked) {
    showToast(blocked);
    render();
    return;
  }
  play("done");
  await slideAway(key);
  moveSelectionOff([key]);
  const h = await actions.perform([{ id: newId(), kind: "done", key, payload: {} }], { onPatched: render });
  const p = h.posted[0];
  if (p && movedOn(p)) {
    await refresh();
    const next = byKey(key);
    showToast(next?.dueAt ? `Done. Next: ${fmtWhenAt(new Date(next.dueAt), now())}` : `Done: ${it.title}`, undoing(h));
    return;
  }
  if (p) showToast(`Done: ${it.title}`, undoing(h), offersTell(it, repos()) ? key : undefined);
  await refresh();
  if (p && allClear()) play("clear");
}

/** Whether the list on screen has nothing left open. */
function allClear(): boolean {
  const groups = currentGroups();
  return groups.length > 0 && !groups.some((g) => g.items.some(isOpen));
}

/** Keys whose rows are sliding away; a render meanwhile draws them hidden. */
const leaving = new Set<string>();
const wait = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/**
 * Slides a row off to the right over the done colour, then folds it shut. It
 * slides on from wherever a swipe let go. Resolves at once where motion is
 * reduced or the row is not on screen.
 */
async function slideAway(key: string): Promise<void> {
  const li = document.querySelector<HTMLElement>(`#list .row[data-key="${CSS.escape(key)}"]`);
  const fg = li?.querySelector<HTMLElement>(".row-fg");
  if (!li || !fg) return;
  if (!li.getClientRects().length || matchMedia("(prefers-reduced-motion: reduce)").matches) {
    fg.style.transform = "";
    li.classList.remove("swiping");
    delete li.dataset.swipe;
    return;
  }
  leaving.add(key);
  li.dataset.swipe = "done";
  li.classList.add("swiping", "leaving");
  void fg.offsetWidth;
  fg.style.transform = "translateX(105%)";
  await wait(SLIDE_MS);
  li.style.height = `${li.offsetHeight}px`;
  void li.offsetHeight;
  li.classList.add("folding");
  li.style.height = "0px";
  await wait(FOLD_MS);
  leaving.delete(key);
}

/**
 * Won't do on an agent item; Delete on a personal one, which then shows
 * nowhere. Its open sub-tasks go with it, closed first, under one Undo.
 */
function doDismiss(key: string) {
  const it = byKey(key);
  if (!it || !isOpen(it) || !allows(it, "dismiss")) return;
  const agent = isAgentItem(it);
  const all = subtasksFirst([it], items(), true);
  const subs = all.length - 1;
  moveSelectionOff(all.map((i) => i.key));
  const text = `${agent ? "Won't do" : "Deleted"}: ${it.title}${subs ? ` and ${subs === 1 ? "its sub-task" : `${subs} sub-tasks`}` : ""}`;
  void closing(all.map((i) => ({ id: newId(), kind: "dismiss", key: i.key, payload: {} })), text, {
    tell: offersTell(it, repos()) ? key : undefined,
    draw: () => (!agent && S.open === key ? closeDetail() : render()),
  });
}

/** Brings a deleted personal item back; Undo deletes it again. */
async function doRestore(key: string) {
  const it = byKey(key);
  if (!it || isOpen(it) || !allows(it, "reopen")) return;
  play("undo");
  await closing([{ id: newId(), kind: "reopen", key, payload: {} }], `Restored: ${it.title}`);
}

async function doReopen(key: string) {
  const it = byKey(key);
  if (!it || isOpen(it) || !allows(it, "reopen")) return;
  play("undo");
  await actions.perform([{ id: newId(), kind: "reopen", key, payload: {} }], { onPatched: render });
  await refresh();
}

function doSnooze(keys: string[], until: Date) {
  const acts: NewAction[] = [];
  for (const key of keys) {
    const it = byKey(key);
    if (!it || !allows(it, "snooze")) continue;
    const payload: Payload = { until: until.toISOString(), previous: it.deferUntil ?? null };
    acts.push({ id: newId(), kind: "snooze", key, payload });
  }
  if (!acts.length) return;
  play("snooze");
  moveSelectionOff(keys);
  const label = fmtWhen(until, now());
  const text = acts.length > 1 ? `Snoozed ${acts.length} tasks to ${label}` : `Snoozed to ${label}`;
  void closing(acts, text, {
    draw: () => {
      S.snoozeKeys = null;
      if (keys === bulkSnoozeKeys) {
        bulkSnoozeKeys = null;
        endSelect();
      } else render();
    },
  });
}

// ---------- select mode ----------

function selectable(): boolean {
  return Boolean(S.state) && (filtering() || canSelect(S.view, S.query));
}

/** Ends select mode where it no longer applies, and drops picks that left the list. */
function keepSelect() {
  if (!S.selecting) return;
  if (!selectable()) {
    S.selecting = false;
    S.picked = new Set();
    return;
  }
  S.picked = keepPickable(S.picked, pickableKeys(currentGroups()));
}

function startSelect() {
  S.selecting = true;
  S.picked = new Set();
  render();
}

/** Renders, putting focus back on the control it was on when the rewrite replaced it. */
function renderKeepingFocus() {
  const el = document.activeElement as HTMLElement | null;
  const act = el?.dataset?.act;
  const key = el?.dataset?.key;
  render();
  if (!act || document.activeElement !== document.body) return;
  const sel = `[data-act="${CSS.escape(act)}"]` + (key ? `[data-key="${CSS.escape(key)}"]` : "");
  document.querySelector<HTMLElement>(`#list ${sel}, #bulkbar ${sel}`)?.focus({ preventScroll: true });
}

/** Leaves select mode; focus left on the bar or a row's checkbox goes back to Select. */
function endSelect() {
  S.selecting = false;
  S.picked = new Set();
  const lost = document.activeElement?.closest("#bulkbar, .pick");
  render();
  if (lost) $<HTMLElement>("#select-btn:not([hidden])")?.focus({ preventScroll: true });
}

function togglePick(key: string) {
  const it = byKey(key);
  if (!it || !isOpen(it)) return;
  if (!S.picked.delete(key)) S.picked.add(key);
  renderKeepingFocus();
}

/** The open picks, in list order. */
function pickedItems(): Item[] {
  return pickableKeys(currentGroups())
    .filter((k) => S.picked.has(k))
    .map(byKey)
    .filter((i): i is Item => Boolean(i) && isOpen(i!));
}

/**
 * Done on every pick, with one Undo: plain items reopen, repeating personal
 * items go back to the date they moved on from.
 */
async function bulkDone() {
  const picked = pickedItems();
  if (!picked.length) return;
  // Picked sub-tasks close before their parents; a parent with a sub-task
  // left open stays.
  const ordered = subtasksFirst(picked, items(), false);
  const closes = new Set(ordered.map((i) => i.key));
  const its = ordered.filter((i) => openSubtasks(i, items()).every((s) => closes.has(s.key)));
  const kept = picked.length - its.length;
  if (kept) showToast(kept === 1 ? "One task has open sub-tasks and stays open." : `${kept} tasks have open sub-tasks and stay open.`);
  if (!its.length) return;
  const keys = its.map((i) => i.key);
  play("done");
  moveSelectionOff(keys);
  const h = await actions.perform(
    keys.map((key) => ({ id: newId(), kind: "done", key, payload: {} })),
    { onPatched: endSelect },
  );
  await refresh();
  if (!h.posted.length) return;
  const done = its.filter((i) => h.posted.some((p) => p.sent.key === i.key));
  showToast(bulkToast("Done", done.map((i) => i.title)), undoing(h));
}

/** Delete or Won't do on every pick, with one Undo. */
function bulkDismiss() {
  const picked = pickedItems();
  if (!picked.length) return;
  const its = subtasksFirst(picked, items(), true);
  const keys = its.map((i) => i.key);
  const verb = bulkDismissDone(picked);
  moveSelectionOff(keys);
  const openGone = S.open && keys.includes(S.open) && isPersonal(byKey(S.open)!);
  void closing(
    keys.map((key) => ({ id: newId(), kind: "dismiss", key, payload: {} })),
    bulkToast(verb, picked.map((i) => i.title)),
    {
      draw: () => {
        endSelect();
        if (openGone) closeDetail();
      },
    },
  );
}

/** The keys the snooze dialog was opened with from the bar; snoozing them ends select mode. */
let bulkSnoozeKeys: string[] | null = null;
function bulkSnooze() {
  const keys = pickedItems().map((i) => i.key);
  if (!keys.length) return;
  openSnooze(keys);
  bulkSnoozeKeys = S.snoozeKeys;
}

async function doNote(key: string) {
  const it = byKey(key);
  if (!it || !allows(it, "note")) return;
  const ta = document.querySelector<HTMLTextAreaElement>("#note-input");
  const text = (ta?.value ?? "").trim();
  if (!text) return;
  if (await post({ id: newId(), kind: "note", key, payload: { text } })) {
    S.drafts.delete(key);
    if (ta) ta.value = "";
    showToast("Note saved");
  }
  await refresh();
}

// ---------- agent runs ----------

/**
 * Wakes the item's agent. With text, the note goes with the ask: on a live
 * run it is a follow-up the agent reads at its next step. A follow-up with
 * no text changes nothing, so the button then only points at the note box.
 */
async function doAsk(key: string, withNote: boolean) {
  const it = byKey(key);
  if (!it || !allows(it, "ask")) return;
  const ta = document.querySelector<HTMLTextAreaElement>("#note-input");
  const text = withNote ? (ta?.value ?? "").trim() : "";
  if (withNote && !text && isWorking(it)) {
    ta?.focus();
    return;
  }
  const who = agentName(it);
  const follow = isWorking(it);
  const payload: Payload = text ? { text } : {};
  if (await post({ id: newId(), kind: "ask", key, payload })) {
    if (text) {
      S.drafts.delete(key);
      if (ta) ta.value = "";
    }
    showToast(follow ? `Sent to ${who}` : `Asked ${who}`);
  }
  await refresh();
}

async function doStop(key: string) {
  const it = byKey(key);
  if (!it) return;
  if (await post({ id: newId(), kind: "stop", key, payload: {} })) showToast(`Stopped ${agentName(it)}`);
  await refresh();
}

/** Tell <agent> in the undo bar: an ask with no text on the item just closed, so its agent reads why. */
async function doTell(key: string) {
  const t = S.toast;
  const it = byKey(key);
  if (!it || !allows(it, "ask")) return;
  await post({ id: newId(), kind: "ask", key, payload: {} });
  await refresh();
  // The bar stays a while longer, now with Stop while the run works.
  if (t && S.toast?.id === t.id) showToast(t.text, t.undo, t.tell);
}

async function switchAgents(bridge: string, repo: string) {
  const r = repos().find((x) => x.bridge === bridge && x.name === repo);
  // The app can only switch agents off; they switch on at the machine (docs/api.md, Agents).
  if (!r?.agentsAllowed) return;
  await post({ id: newId(), kind: "agents", bridge, repo, payload: { on: false } });
  await refresh();
}

// ---------- editing personal items ----------

/** Performs an action on a personal item, offering Undo with `toast` when given. */
async function postEdit(a: NewAction, toast?: string) {
  const h = await actions.perform([a], { onPatched: render });
  if (h.posted.length && toast) showToast(toast, undoing(h));
  await refresh();
}

/**
 * Saves one change of a personal item as an edit holding only what changed,
 * with Undo. Returns why it cannot be saved, or "".
 */
function saveField(key: string, change: (d: EditDraft) => void): string {
  const it = byKey(key);
  if (!it || !canEdit(it)) return "";
  const d = draftOf(it, browserZone());
  change(d);
  const err = editError(it, d, now());
  if (err) return err;
  const set = diffEdit(it, d, now());
  if (set) void postEdit({ id: newId(), kind: "edit", key, payload: { set } }, "Saved");
  return "";
}

/** The field each inline edit writes in. */
const INLINE_FIELD: Record<Inline["field"], string> = { title: "#d-title-input", description: "#d-desc", tags: "#p-tags" };
/** What focus goes back to once an inline edit ends. */
const INLINE_RETURN: Record<Inline["field"], string> = {
  title: ".d-title-btn",
  description: "[data-act=inline][data-field=description]",
  tags: "[data-act=inline][data-field=tags]",
};

/** Turns the title, details or tags of the open personal item into a field. */
function startInline(field: Inline["field"]) {
  const it = byKey(S.open);
  if (!it || !canEdit(it)) return;
  const text = field === "title" ? it.title : field === "description" ? (it.description ?? "") : shownTags(it).join(" ");
  S.inline = { key: it.key, field, text, error: "" };
  S.noteEdit = null;
  S.moreOpen = false;
  render();
  const f = $<HTMLInputElement>(INLINE_FIELD[field]);
  f.focus();
  if (field !== "description") f.select();
}

/** Ends an inline edit without saving, and puts focus back where it started. */
function cancelInline() {
  const e = S.inline;
  S.inline = null;
  render();
  if (e) document.querySelector<HTMLElement>(`#detail ${INLINE_RETURN[e.field]}`)?.focus({ preventScroll: true });
}

/** Saves an inline edit; a value the hub would refuse keeps the field open with the reason. */
function commitInline() {
  const e = S.inline;
  if (!e) return;
  const err = saveField(e.key, (d) => {
    if (e.field === "title") d.title = e.text;
    else if (e.field === "description") d.description = e.text;
    else d.tags = e.text;
  });
  if (err) {
    e.error = err;
    render();
    document.querySelector<HTMLElement>(INLINE_FIELD[e.field])?.focus();
    return;
  }
  S.inline = null;
  render();
  document.querySelector<HTMLElement>(`#detail ${INLINE_RETURN[e.field]}`)?.focus({ preventScroll: true });
}

/** A row's select changed: it saves at once. */
function propChanged(t: HTMLSelectElement) {
  if (!S.open) return;
  const v = t.value;
  const err = saveField(S.open, (d) => {
    if (t.id === "p-prio") d.priority = Number(v);
    else if (t.id === "p-lead") d.remindBefore = v === "" ? null : Number(v);
    else if (t.id === "p-parent") d.parent = v;
  });
  if (err) {
    showToast(err);
    render();
  }
}

function toggleMore(show = !S.moreOpen) {
  S.moreOpen = show;
  render();
  if (show) document.querySelector<HTMLElement>("#d-more-menu .more-item")?.focus();
}

// ---------- pages inside the sheet ----------

/** Opens the Schedule page of the open personal item, with a draft of its schedule. */
function openSchedule(key: string) {
  const it = byKey(key);
  if (!it || !canEdit(it)) return;
  const draft = draftOf(it, browserZone());
  S.page = { kind: "schedule", key, draft, base: draft, typed: "", typedError: "", error: "", ask: null };
  S.inline = null;
  S.moreOpen = false;
  S.noteEdit = null;
  // The lead time choice names the hub's default.
  loadLead(ctx);
  render();
  $("#detail").scrollTop = 0;
  // On a phone the keyboard would cover the controls, so focus waits on the title.
  (isPhone() ? $<HTMLElement>("#pg-h") : $<HTMLInputElement>("#sched-typed")).focus();
}

function openSnoozePage(key: string) {
  const it = byKey(key);
  if (!it || !isOpen(it) || !allows(it, "snooze")) return;
  snoozeTyped.delete("d-type");
  S.page = { kind: "snooze", key };
  S.inline = null;
  S.moreOpen = false;
  render();
  $("#detail").scrollTop = 0;
  $<HTMLElement>("#detail .snz").focus();
}

/** Back to the task, with focus on what opened the page. */
function closePage() {
  const p = S.page;
  if (!p) return;
  S.page = null;
  render();
  document.getElementById(p.kind === "schedule" ? "d-sched" : "d-snooze")?.focus({ preventScroll: true });
}

/** After a snooze from its page, back to the task. */
function leaveSnoozePage() {
  if (S.page?.kind !== "snooze") return;
  S.page = null;
  render();
  document.getElementById("d-snooze")?.focus({ preventScroll: true });
}

function schedulePage(): SchedulePage | null {
  return S.page?.kind === "schedule" ? S.page : null;
}

/** The typed field changed: it sets the draft from the one the controls last made. */
function scheduleTyped(text: string) {
  const p = schedulePage();
  if (!p) return;
  p.typed = text;
  const r = readTyped(text, now(), p.base);
  p.typedError = r && "error" in r ? r.error : "";
  p.draft = r && "draft" in r ? r.draft : p.base;
  p.error = "";
  p.ask = null;
  render();
}

/** A control changed the draft: the typed text no longer says what it holds, so it clears. */
function scheduleControl(change: (d: EditDraft) => EditDraft | void) {
  const p = schedulePage();
  if (!p) return;
  const d: EditDraft = { ...p.draft, weekdays: [...p.draft.weekdays] };
  p.draft = change(d) ?? d;
  p.base = p.draft;
  p.typed = "";
  p.typedError = "";
  p.error = "";
  p.ask = null;
  render();
}

/** Save on the Schedule page: one edit, or first the question a moved repeating task asks. */
function saveSchedule() {
  const p = schedulePage();
  const it = byKey(p?.key ?? null);
  if (!p || !it) return;
  if (p.typedError) {
    p.error = p.typedError;
    render();
    $<HTMLInputElement>("#sched-typed").focus();
    return;
  }
  const r = scheduleSave(it, p.draft, now(), allows(it, "snooze"));
  switch (r.kind) {
    case "none":
      closePage();
      return;
    case "error": {
      p.error = r.text;
      render();
      const field = /^Repeat every/.test(r.text) ? "#sched-interval" : /^End the repeat/.test(r.text) ? "#sched-until" : "#sched-date";
      document.querySelector<HTMLElement>(field)?.focus();
      return;
    }
    case "ask":
      p.ask = { set: r.set, until: r.until };
      render();
      $<HTMLElement>("#ask-h").focus();
      return;
    case "edit":
      closePage();
      void postEdit({ id: newId(), kind: "edit", key: it.key, payload: { set: r.set } }, "Saved");
  }
}

/** The answer to "Just this one, or from now on?". */
function answerAsk(once: boolean) {
  const p = schedulePage();
  if (!p?.ask) return;
  const { set, until } = p.ask;
  const key = p.key;
  closePage();
  if (once) doSnooze([key], until);
  else void postEdit({ id: newId(), kind: "edit", key, payload: { set } }, "Saved");
}

function openNoteEdit(key: string, commentId: string) {
  const c = byKey(key)?.comments?.find((x) => x.id === commentId);
  if (!c) return;
  S.noteEdit = { key, commentId, text: c.text };
  S.inline = null;
  render();
  $<HTMLTextAreaElement>("#note-edit-input").focus();
}

function closeNoteEdit() {
  const id = S.noteEdit?.commentId;
  S.noteEdit = null;
  render();
  if (id) document.getElementById(`ne-${id}`)?.focus({ preventScroll: true });
}

function saveNoteEdit() {
  const n = S.noteEdit;
  const c = byKey(n?.key ?? null)?.comments?.find((x) => x.id === n?.commentId);
  if (!n || !c) return;
  const text = n.text.trim();
  if (!text) {
    $<HTMLTextAreaElement>("#note-edit-input").focus();
    return;
  }
  closeNoteEdit();
  if (text === c.text) return;
  const a: NewAction = { id: newId(), kind: "note-edit", key: n.key, payload: { commentId: c.id, text } };
  void postEdit(a, "Note saved");
}

function deleteNote(key: string, commentId: string) {
  if (S.noteEdit?.commentId === commentId) S.noteEdit = null;
  const a: NewAction = { id: newId(), kind: "note-delete", key, payload: { commentId } };
  void postEdit(a, "Note deleted");
  $<HTMLElement>("#note-input").focus({ preventScroll: true });
}

/**
 * Whether a sub-task row offers its remove button: Delete on the hub's own
 * lists, open or done, and Won't do on an open one of a tracker that can
 * dismiss. One still being created has none.
 */
function canRemoveSubtask(c: Item): boolean {
  if (c.id.startsWith("pending-") || !allows(c, "dismiss")) return false;
  return isOpen(c) || !isAgentItem(c);
}

/**
 * The remove button on a sub-task row: the same dismiss as Delete or Won't
 * do, with Undo. A done sub-task of the hub's lists comes back done.
 */
async function removeSubtask(key: string) {
  const it = byKey(key);
  if (!it || !canRemoveSubtask(it)) return;
  const agent = isAgentItem(it);
  const wasDone = !isOpen(it);
  const h = await actions.perform([{ id: newId(), kind: "dismiss", key, payload: {} }], {
    onPatched: () => {
      render();
      // The row is gone; keep focus in the sub-tasks.
      document.querySelector<HTMLElement>("#sub-input")?.focus({ preventScroll: true });
    },
  });
  if (h.posted.length) {
    const undo = undoing(h);
    const back =
      undo && wasDone
        ? async () => {
            await undo();
            if (await post({ id: newId(), kind: "done", key, payload: {} })) await refresh();
          }
        : undo;
    showToast(`${agent ? "Won't do" : "Deleted"}: ${it.title}`, back);
  }
  await refresh();
}

/** Adds a sub-task to an item, in the item's own list. */
async function addSubtask(key: string) {
  const it = byKey(key);
  const input = document.querySelector<HTMLInputElement>("#sub-input");
  if (!it || !input || !allows(it, "subtask")) return;
  const title = input.value.trim();
  if (!title) return;
  input.value = "";
  S.subDrafts.delete(key);
  if (await post({ id: newId(), kind: "add", bridge: it.bridge, repo: it.repo, payload: { title, due: null, parent: it.id } })) await refresh();
  document.querySelector<HTMLElement>("#sub-input")?.focus({ preventScroll: true });
}

/** What a close of `it` waits for while sub-tasks are open, or "" when it can close. */
function blockedBySubtasks(it: Item): string {
  const n = openSubtasks(it, items()).length;
  if (!n) return "";
  return n === 1 ? "Finish or remove its open sub-task first." : `Finish or remove its ${n} open sub-tasks first.`;
}

// ---------- toast ----------

let toastSeq = 0;
let toastTimer: ReturnType<typeof setTimeout> | undefined;
function showToast(text: string, undo?: () => Promise<void>, tell?: string) {
  clearTimeout(toastTimer);
  const id = ++toastSeq;
  S.toast = { id, text, undo, tell };
  renderToast();
  toastTimer = setTimeout(() => {
    if (S.toast?.id === id) {
      S.toast = null;
      renderToast();
    }
  }, UNDO_MS);
}

// ---------- navigation ----------

function setView(v: View) {
  S.view = v;
  S.menuOpen = false;
  S.selected = null;
  S.selecting = false;
  S.picked = new Set();
  // Picking a task view shows that view, so it ends a search; Settings keeps it for the way back.
  if (!isPage(v) && S.query) {
    S.query = "";
    $<HTMLInputElement>("#search-input").value = "";
  }
  // So does a tag filter.
  if (!isPage(v)) S.tag = null;
  if (isPhone()) closeDetail();
  if (v !== "settings") leaveSettings();
  // A new view sets the New task list afresh: the list being viewed, else the default.
  qaListTouched = false;
  if (v === "month" && !S.month) S.month = { month: monthOf(now()), selected: startOfDay(now()) };
  render();
  updateQuickAdd();
  loadOccurrences();
  $("#list").scrollTop = 0;
  $("#wizard").scrollTop = 0;
}

function openItem(key: string) {
  const it = byKey(key);
  if (!it) return;
  if (S.open !== key) {
    S.page = null;
    S.inline = null;
    S.moreOpen = false;
    S.noteEdit = null;
  }
  // The Remind early row names the hub's default.
  if (canEdit(it)) loadLead(ctx);
  S.open = key;
  S.selected = key;
  S.openSeenAt = it.seenAt;
  setURL(key);
  render();
  void api.seen(key).then(refresh, () => {});
  if (isPhone()) $<HTMLElement>("#detail [data-act=close-detail]")?.focus();
}

function closeDetail() {
  if (!S.open) return;
  const key = S.open;
  S.open = null;
  S.page = null;
  S.inline = null;
  S.moreOpen = false;
  S.noteEdit = null;
  setURL(null);
  render();
  const row = document.querySelector<HTMLElement>(`#list .row[data-key="${CSS.escape(key)}"] .row-body`);
  row?.focus({ preventScroll: true });
}

function setURL(key: string | null) {
  const u = new URL(location.href);
  if (key) u.searchParams.set("item", key);
  else u.searchParams.delete("item");
  u.searchParams.delete("reply");
  history.replaceState(null, "", u);
}

let snoozeReturn: HTMLElement | null = null;
function openSnooze(keys: string[]) {
  keys = keys.filter((k) => {
    const it = byKey(k);
    return it && allows(it, "snooze");
  });
  if (!keys.length) return;
  snoozeReturn = document.activeElement as HTMLElement | null;
  snoozeTyped.delete("dlg-type");
  S.snoozeKeys = keys;
  render();
  $<HTMLElement>("#snooze .snz")?.focus();
}
function closeSnooze() {
  S.snoozeKeys = null;
  render();
  snoozeReturn?.focus?.({ preventScroll: true });
  snoozeReturn = null;
}

function selectBy(delta: number) {
  const keys = visibleKeys();
  if (!keys.length) return;
  const idx = S.selected ? keys.indexOf(S.selected) : -1;
  const next = idx < 0 ? (delta > 0 ? 0 : keys.length - 1) : Math.max(0, Math.min(keys.length - 1, idx + delta));
  S.selected = keys[next];
  applySelection();
  const btn = document.querySelector<HTMLElement>(`#list .row[data-key="${CSS.escape(S.selected)}"] .row-body`);
  btn?.focus({ preventScroll: true });
  btn?.scrollIntoView({ block: "nearest" });
}

// ---------- quick add ----------

let qa: QuickAdd = { title: "", due: null, repeat: null, list: null, tags: [], tokens: [] };
let qaListTouched = false;
/** The New task form's agent switch as the user set it; null is its default, off, so Add wakes no agent until it is switched on. */
let qaAsk: boolean | null = null;

/** Whether Add will also wake an agent on the new task. */
function qaWakes(listKey: string): boolean {
  return wakesAgent(repos().find((x) => repoKey(x) === listKey), qaAsk ?? false);
}

/**
 * The Agent pill, "An agent starts on it", shown only on a list where an
 * agent may work. While it is on, the line under the bar says what happens
 * and Add turns pink.
 */
function updateQaAgent(listKey: string) {
  const box = $("#qa-agent");
  const why = $("#qa-agent-why");
  const add = $("#qa-add");
  const r = repos().find((x) => repoKey(x) === listKey);
  const show = !!r && !noAgentReason(r);
  const on = show && qaWakes(listKey);
  box.hidden = !show;
  setHTML(
    box,
    show
      ? `<button type="button" class="qa-pill qa-ask" role="switch" id="qa-ask" data-act="qa-ask" aria-checked="${on}" aria-label="An agent starts on it" aria-describedby="qa-agent-why">${ICON.spark}Agent</button>`
      : "",
  );
  why.hidden = !on;
  why.textContent = on && r ? `It reads the task in ${repoLabel(r)} and replies here.` : "";
  add.classList.toggle("btn-agent", on);
  add.classList.toggle("btn-primary", !on);
  setHTML(add, `${on ? ICON.spark : ICON.plus}Add`);
}

/** The lists a new task can go to: those whose tracker takes adds. */
function addable(): Repo[] {
  return repos().filter((r) => repoCan(r, "add"));
}

/** The list being viewed, as a repo key, so a new task starts there. */
function viewingList(): string | undefined {
  return S.view.startsWith("list:") ? S.view.slice("list:".length) : undefined;
}

function updateQuickAdd() {
  if (!shellUp) return;
  const input = $<HTMLInputElement>("#qa-input");
  const select = $<HTMLSelectElement>("#qa-list");
  const opts = visibleRepos(addable())
    .map((r) => `<option value="${esc(repoKey(r))}">${esc(listLabel(r))}</option>`)
    .join("");
  const prev = select.value;
  setHTML(select, opts);
  // No lists yet (no machine paired): hide the picker rather than show it blank.
  select.hidden = !opts;
  // The list the text names, else a pick the user made, else the list being
  // viewed, else the personal list; a stale pick falls back the same way.
  const read = readQuickAdd(input.value, now(), addable(), { list: qaListTouched ? prev : undefined, viewing: viewingList() });
  qa = read.qa;
  select.value = read.target ? repoKey(read.target) : "";

  updateQaAgent(select.value);

  // Highlight recognised spans behind the input.
  let html = "";
  let pos = 0;
  for (const t of qa.tokens) {
    html += esc(input.value.slice(pos, t.start)) + `<mark class="hl-${t.kind}">${esc(input.value.slice(t.start, t.end))}</mark>`;
    pos = t.end;
  }
  html += esc(input.value.slice(pos));
  setHTML($("#qa-backdrop"), html + " ");
  $("#qa-backdrop").scrollLeft = input.scrollLeft;

  const chips: string[] = [];
  if (qa.due) chips.push(`<span class="chip chip-date">${esc(dueLabel(qa.due, now()))}</span>`);
  if (qa.repeat) chips.push(`<span class="chip chip-repeat">${ICON.repeat}${esc(repeatLabel(qa.repeat, now()))}</span>`);
  if (qa.early) chips.push('<span class="chip chip-error">Ends before it starts</span>');
  if (qa.list) {
    const r = repos().find((x) => repoKey(x) === qa.list);
    chips.push(`<span class="chip chip-list">${esc(r ? listLabel(r) : qa.list)}</span>`);
  }
  for (const t of qa.tags) chips.push(`<span class="chip chip-tag">${ICON.tag}${esc(t)}</span>`);
  setHTML($("#qa-chips"), chips.join(""));
}

async function submitQuickAdd() {
  const input = $<HTMLInputElement>("#qa-input");
  updateQuickAdd();
  const add = addAction(input.value, now(), addable(), {
    id: newId(),
    list: $<HTMLSelectElement>("#qa-list").value,
    viewing: viewingList(),
    ask: qaAsk ?? false,
    zone: browserZone(),
  });
  if ("error" in add) {
    if (add.error !== "empty") showToast(add.why ?? ADD_ERROR_TEXT[add.error]);
    if (add.error !== "no-list") input.focus();
    return;
  }
  const { title } = add.qa;
  const { wakes } = add;
  if (await post(add.action)) {
    play("add");
    input.value = "";
    qaListTouched = false;
    qaAsk = null;
    showToast(wakes ? `Added and asked an agent: ${title}` : `Added: ${title}`);
    if (isPhone()) closeQuickAdd();
  }
  await refresh();
}

function openQuickAdd() {
  if (isPhone()) {
    S.qaOpen = true;
    render();
  }
  if (isPage(S.view)) setView("today");
  $<HTMLInputElement>("#qa-input").focus();
}

function closeQuickAdd() {
  S.qaOpen = false;
  render();
  $<HTMLElement>(".fab").focus({ preventScroll: true });
}

// ---------- push ----------

function resubscribeOnOpen() {
  if (!remindersOn() || !("serviceWorker" in navigator)) return;
  subscribePush().catch(() => {});
}

// ---------- events ----------

/** When a swipe ended: the click a mouse drag fires on the list right after is not a tap. */
let swipeEnded = 0;

function onClick(e: MouseEvent) {
  if (!shellUp) return;
  // The click a long press's release may fire lands on the Move dialog's scrim or the list: it is not a tap.
  if (Date.now() - longPressEnded < 300) {
    longPressEnded = 0;
    e.preventDefault();
    e.stopPropagation();
    return;
  }
  if (Date.now() - swipeEnded < 400 && (e.target as HTMLElement).closest("#list")) {
    swipeEnded = 0;
    e.preventDefault();
    e.stopPropagation();
    return;
  }
  const target = e.target as HTMLElement;
  if (target.id === "scrim") {
    if (S.snoozeKeys) closeSnooze();
    else if (S.move) closeMove();
    else if (S.qaOpen) closeQuickAdd();
    else if (S.menuOpen) closeMenu();
    else closeDetail();
    return;
  }
  if (S.syncOpen && !target.closest("#syncpop") && !target.closest(".sync-pill")) {
    S.syncOpen = false;
    renderSync();
  }
  // A tap outside the More menu closes it.
  if (S.moreOpen && !target.closest("#d-more-menu") && !target.closest("#d-more")) {
    S.moreOpen = false;
    render();
  }
  const btn = target.closest<HTMLElement>("[data-act]");
  if (!btn) return;
  const key = btn.dataset.key ?? "";
  switch (btn.dataset.act) {
    case "view":
      setView(btn.dataset.view as View);
      break;
    case "tag": {
      // A tag chip on a row sits inside the row's button: it filters, and the row stays shut.
      const rowKey = btn.closest<HTMLElement>(".row")?.dataset.key;
      if (S.selecting && rowKey) togglePick(rowKey);
      else filterTag(btn.dataset.tag ?? null);
      break;
    }
    case "tag-clear":
      filterTag(null);
      break;
    case "open":
      // In select mode a tap on a row picks it.
      if (S.selecting) togglePick(key);
      else openItem(key);
      break;
    case "pick":
      togglePick(key);
      break;
    case "select-mode":
      if (S.selecting) endSelect();
      else startSelect();
      break;
    case "bulk-done":
      void bulkDone();
      break;
    case "bulk-snooze":
      bulkSnooze();
      break;
    case "bulk-dismiss":
      bulkDismiss();
      break;
    case "bulk-all":
      S.picked = new Set(pickableKeys(currentGroups()));
      renderKeepingFocus();
      break;
    case "bulk-cancel":
      endSelect();
      break;
    case "close-detail":
      closeDetail();
      break;
    case "done":
      doDone(key);
      break;
    case "dismiss":
      doDismiss(key);
      break;
    case "reopen":
      void doReopen(key);
      break;
    case "restore":
      void doRestore(key);
      break;
    case "note":
      void doNote(key);
      break;
    case "ask":
      void doAsk(key, false);
      break;
    case "ask-note":
      void doAsk(key, true);
      break;
    case "stop":
      void doStop(key);
      break;
    case "tell":
      void doTell(key);
      break;
    case "agents-settings":
      ctx.openSettings("s-mach");
      break;
    case "agents-switch":
      void switchAgents(btn.dataset.bridge ?? "", btn.dataset.repo ?? "");
      break;
    case "agents-on-copy":
      void copyText(btn.dataset.copy ?? "").then(() => showToast("Copied"));
      break;
    case "inline":
      startInline(btn.dataset.field as Inline["field"]);
      break;
    case "schedule":
      openSchedule(key);
      break;
    case "snooze-page":
      openSnoozePage(key);
      break;
    case "page-back":
      closePage();
      break;
    case "more":
      toggleMore();
      break;
    case "copy-id": {
      const it = byKey(key);
      S.moreOpen = false;
      render();
      $<HTMLElement>("#d-more").focus();
      if (it) void copyText(it.id).then(() => showToast(`Copied ${it.id}`));
      break;
    }
    case "sched-clear-date":
      // The hub ends a repeat with its due date.
      scheduleControl((d) => ({ ...d, date: "", time: "", repeat: "" }));
      $<HTMLInputElement>("#sched-date").focus();
      break;
    case "sched-clear-until":
      scheduleControl((d) => ({ ...d, until: "" }));
      $<HTMLInputElement>("#sched-until").focus();
      break;
    case "sched-once":
      answerAsk(true);
      break;
    case "sched-all":
      answerAsk(false);
      break;
    case "sched-ask-cancel": {
      const p = schedulePage();
      if (p) p.ask = null;
      render();
      $<HTMLInputElement>("#sched-typed").focus();
      break;
    }
    case "note-edit":
      if (S.open) openNoteEdit(S.open, btn.dataset.id ?? "");
      break;
    case "note-edit-cancel":
      closeNoteEdit();
      break;
    case "note-delete":
      if (S.open) deleteNote(S.open, btn.dataset.id ?? "");
      break;
    case "sub-remove":
      void removeSubtask(key);
      break;
    case "later":
      S.laterOpen = !S.laterOpen;
      render();
      break;
    case "snooze-all":
      doSnooze(
        pastDue(items(), now()).map((i) => i.key),
        tomorrowMorning(now()),
      );
      break;
    case "snooze-opt": {
      const keys = btn.closest("#snooze") ? S.snoozeKeys ?? [] : S.open ? [S.open] : [];
      doSnooze(keys, new Date(btn.dataset.until!));
      if (!btn.closest("#snooze")) leaveSnoozePage();
      break;
    }
    case "snooze-cal":
      openSnoozePicker(btn);
      break;
    case "snooze-close":
      closeSnooze();
      break;
    case "month-day":
      selectDay(btn.dataset.day ?? "");
      break;
    case "month-prev":
    case "month-next":
      if (S.month) showMonth(addMonths(S.month.month, btn.dataset.act === "month-prev" ? -1 : 1));
      break;
    case "month-today":
      showMonth(monthOf(now()));
      break;
    case "month-move":
      openMove(btn);
      break;
    case "move-close":
      closeMove();
      break;
    case "undo": {
      const t = S.toast;
      if (t?.undo) {
        S.toast = null;
        renderToast();
        play("undo");
        void t.undo();
      }
      break;
    }
    case "new":
      openQuickAdd();
      break;
    case "qa-close":
      closeQuickAdd();
      break;
    case "qa-ask":
      qaAsk = !qaWakes($<HTMLSelectElement>("#qa-list").value);
      updateQuickAdd();
      break;
    case "menu":
      if (S.menuOpen) closeMenu();
      else openMenu();
      break;
    case "sync":
      S.syncOpen = !S.syncOpen;
      syncActions.forget();
      renderSync();
      if (S.syncOpen) {
        void syncActions.reload(renderSync);
        $<HTMLElement>("#syncpop [data-act=sync-close]")?.focus();
      }
      break;
    case "sync-close":
      S.syncOpen = false;
      renderSync();
      break;
    case "theme":
      setTheme(getTheme() === "dark" ? "light" : "dark");
      renderTheme();
      break;
    case "logout":
      void logout();
      break;
    case "sync-switch":
      void switchSync(btn.dataset.bridge ?? "", btn.dataset.repo ?? "");
      break;
    default:
      void extAct(btn, ctx);
  }
}

function isTyping(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || (el as HTMLElement).isContentEditable;
}

function onKey(e: KeyboardEvent) {
  if (!shellUp) return;
  if (e.key === "Tab" && (S.snoozeKeys || S.move)) {
    // Keep focus inside the modal dialog.
    const box = S.snoozeKeys ? "#snooze" : "#move";
    const f = [...document.querySelectorAll<HTMLElement>(`${box} button, ${box} input`)];
    if (!f.length) return;
    const i = f.indexOf(document.activeElement as HTMLElement);
    const next = e.shiftKey ? (i <= 0 ? f.length - 1 : i - 1) : i < 0 || i === f.length - 1 ? 0 : i + 1;
    f[next].focus();
    e.preventDefault();
    return;
  }
  if (e.key === "Escape") {
    if (S.snoozeKeys) closeSnooze();
    else if (S.move) closeMove();
    else if (S.syncOpen) {
      S.syncOpen = false;
      renderSync();
      $<HTMLElement>(".sync-pill:not([hidden])")?.focus();
    } else if (S.menuOpen) closeMenu();
    else if (S.qaOpen) closeQuickAdd();
    else if (S.noteEdit && S.open) closeNoteEdit();
    else if (S.moreOpen) {
      S.moreOpen = false;
      render();
      $<HTMLElement>("#d-more").focus();
    } else if (S.inline) cancelInline();
    else if (S.page?.kind === "schedule" && S.page.ask) {
      S.page.ask = null;
      render();
      $<HTMLInputElement>("#sched-typed").focus();
    } else if (S.page) closePage();
    else if (document.activeElement?.id === "search-input") {
      S.query = "";
      (document.activeElement as HTMLInputElement).value = "";
      (document.activeElement as HTMLElement).blur();
      render();
    } else if (isTyping(document.activeElement)) (document.activeElement as HTMLElement).blur();
    else if (S.selecting) endSelect();
    else if (S.open) closeDetail();
    else if (filtering()) filterTag(null);
    else return;
    e.preventDefault();
    return;
  }
  if (e.ctrlKey || e.metaKey || e.altKey || isTyping(document.activeElement)) return;
  if (S.snoozeKeys || S.move) return;
  const active = document.activeElement as HTMLElement | null;
  switch (e.key) {
    case "j":
      selectBy(1);
      break;
    case "k":
      selectBy(-1);
      break;
    case "x":
      if (S.selecting && S.selected) togglePick(S.selected);
      else if (S.selected) {
        const it = byKey(S.selected);
        if (it && isOpen(it)) doDone(it.key);
        else if (it && isDeleted(it)) void doRestore(it.key);
        else if (it) void doReopen(it.key);
      }
      break;
    case "s":
      if (S.selected && byKey(S.selected) && isOpen(byKey(S.selected)!)) openSnooze([S.selected]);
      break;
    case "n":
      openQuickAdd();
      break;
    case "/":
      if (isPage(S.view)) return;
      $("#search-input").focus();
      break;
    case "Enter":
      if (S.selected && (!active || active === document.body || active.classList.contains("row-body"))) openItem(S.selected);
      else return;
      break;
    default:
      return;
  }
  e.preventDefault();
}

function onInput(e: Event) {
  const t = e.target as HTMLElement;
  if (t.id === "search-input") {
    S.query = (t as HTMLInputElement).value;
    render();
  } else if (t.id === "qa-input") updateQuickAdd();
  else if (t.dataset.typeTime !== undefined) updateSnoozeType(t as HTMLInputElement);
  else if (t.id === "note-input" && S.open) S.drafts.set(S.open, (t as HTMLTextAreaElement).value);
  else if (t.id === "sub-input" && S.open) S.subDrafts.set(S.open, (t as HTMLInputElement).value);
  else if (t.id === "note-edit-input" && S.noteEdit) S.noteEdit.text = (t as HTMLTextAreaElement).value;
  else if (S.inline && t.id === INLINE_FIELD[S.inline.field].slice(1)) S.inline.text = (t as HTMLInputElement).value;
  else if (t.id === "sched-typed") scheduleTyped((t as HTMLInputElement).value);
  else if (t.id === "sched-date" || t.id === "sched-time" || t.id === "sched-interval" || t.id === "sched-until") {
    const v = (t as HTMLInputElement).value;
    scheduleControl((d) => {
      if (t.id === "sched-date") {
        d.date = v;
        // A date picked with no time yet takes the hour quick add gives one.
        if (v && !d.time) d.time = "09:00";
      } else if (t.id === "sched-time") d.time = v;
      else if (t.id === "sched-interval") d.interval = v;
      else d.until = v;
    });
  }
}

function onChange(e: Event) {
  const t = e.target as HTMLElement;
  if (t.dataset.pick !== undefined) snoozePicked(t as HTMLInputElement);
  else if (t.id === "p-prio" || t.id === "p-lead" || t.id === "p-parent") propChanged(t as HTMLSelectElement);
  else if (schedulePage() && t.closest("#sched-form")) scheduleChanged(t as HTMLInputElement);
  else if (t.id === "qa-list") {
    qaListTouched = true;
    updateQaAgent((t as HTMLSelectElement).value);
  }
  else if (shellUp) extChange(e.target as HTMLElement, ctx);
}

/** A radio, checkbox or select of the Schedule page changed. */
function scheduleChanged(t: HTMLInputElement) {
  const v = t.value;
  if (t.name === "sched-repeat") scheduleControl((d) => ({ ...d, repeat: v as EditDraft["repeat"] }));
  else if (t.name === "sched-from") scheduleControl((d) => fitWeekdays({ ...d, fromDone: v === "done" }, now()));
  else if (t.dataset.weekday !== undefined) {
    const n = Number(t.dataset.weekday);
    scheduleControl((d) => fitWeekdays({ ...d, weekdays: t.checked ? [...d.weekdays, n] : d.weekdays.filter((x) => x !== n) }, now()));
  } else if (t.id === "sched-lead") scheduleControl((d) => ({ ...d, remindBefore: v === "" ? null : Number(v) }));
}

/** Leaving an inline field saves it, unless focus only moved within its form. */
function onFocusOut(e: FocusEvent) {
  if (writing || !S.inline) return;
  const t = e.target as HTMLElement;
  if (t.id !== INLINE_FIELD[S.inline.field].slice(1)) return;
  commitInline();
}

function onSubmit(e: SubmitEvent) {
  const form = e.target as HTMLFormElement;
  e.preventDefault();
  if (form.dataset.form === "login") void submitLogin(form);
  else if (form.dataset.form === "pw-claim" && form.closest(".claim")) void submitClaim(form);
  else if (form.id === "qa-form") void submitQuickAdd();
  else if (form.id === "search-form") (document.activeElement as HTMLElement | null)?.blur();
  else if (form.dataset.form === "inline") commitInline();
  else if (form.dataset.form === "schedule") saveSchedule();
  else if (form.dataset.form === "note-edit") saveNoteEdit();
  else if (form.dataset.form === "sub-add" && S.open) void addSubtask(S.open);
  else if (form.dataset.form === "snooze-type") submitSnoozeType(form);
  else if (form.dataset.form === "move") submitMove();
  else if (shellUp) void extSubmit(form, ctx);
}

// Swipe on phone rows: right = done, left = snooze, each where the row's list can apply it.
function setupSwipe(list: HTMLElement) {
  let start: { x: number; y: number; id: number; li: HTMLElement; fg: HTMLElement; done: boolean; snooze: boolean } | null = null;
  let dx = 0;
  let swiping = false;

  const reset = () => {
    if (start) {
      start.fg.style.transform = "";
      start.li.classList.remove("swiping");
      delete start.li.dataset.swipe;
    }
    start = null;
    swiping = false;
    dx = 0;
  };

  list.addEventListener("pointerdown", (e) => {
    if (!isPhone() || S.selecting || (e.pointerType === "mouse" && e.button !== 0)) return;
    const fg = (e.target as HTMLElement).closest<HTMLElement>(".row-fg");
    const li = fg?.closest<HTMLElement>(".row");
    if (!fg || !li || li.classList.contains("is-done")) return;
    const it = byKey(li.dataset.key ?? null);
    if (!it) return;
    start = { x: e.clientX, y: e.clientY, id: e.pointerId, li, fg, done: allows(it, "done"), snooze: allows(it, "snooze") };
    dx = 0;
    swiping = false;
  });
  list.addEventListener("pointermove", (e) => {
    if (!start || e.pointerId !== start.id) return;
    const mx = e.clientX - start.x;
    const my = e.clientY - start.y;
    if (!swiping) {
      if (Math.abs(my) > 10 && Math.abs(my) > Math.abs(mx)) {
        reset();
        return;
      }
      if (Math.abs(mx) < 10) return;
      swiping = true;
      start.li.classList.add("swiping");
      try {
        start.fg.setPointerCapture(e.pointerId);
      } catch {
        // pointer already gone
      }
    }
    // A direction the list cannot apply does not move.
    dx = (mx > 0 && !start.done) || (mx < 0 && !start.snooze) ? 0 : mx;
    start.fg.style.transform = `translateX(${dx}px)`;
    start.li.dataset.swipe = dx > 0 ? "done" : "snooze";
    start.li.classList.toggle("armed", Math.abs(dx) >= SWIPE_THRESHOLD);
  });
  const finish = (e: PointerEvent) => {
    if (!start || e.pointerId !== start.id) return;
    const key = start.li.dataset.key!;
    const wasSwiping = swiping;
    const dist = dx;
    start.li.classList.remove("armed");
    // A done swipe leaves the row where the finger let go, so it slides on from there.
    if (wasSwiping && dist >= SWIPE_THRESHOLD) {
      start = null;
      swiping = false;
      dx = 0;
    } else reset();
    if (!wasSwiping) return;
    swipeEnded = Date.now();
    if (dist >= SWIPE_THRESHOLD) doDone(key);
    else if (dist <= -SWIPE_THRESHOLD) {
      S.selected = key;
      openSnooze([key]);
    }
  };
  list.addEventListener("pointerup", finish);
  list.addEventListener("pointercancel", (e) => {
    if (start && e.pointerId === start.id) reset();
  });
}

// ---------- boot ----------

function shell(): string {
  return `<div class="shell">
<aside id="sidebar" class="sidebar" aria-label="Navigation">
  <button class="btn btn-primary new-task" data-act="new">${ICON.plus}<span>New task</span><kbd aria-hidden="true">n</kbd></button>
  <nav id="nav" class="nav" aria-label="Views"></nav>
  <h2 class="side-h">Lists</h2>
  <nav id="lists" class="nav" aria-label="Lists"></nav>
  <div class="side-foot">
    <button class="sync-pill" data-act="sync" aria-controls="syncpop" aria-haspopup="dialog"></button>
    <div class="foot-row"><button class="link-btn" data-act="view" data-view="settings">${ICON.gear}<span>Settings</span></button><button class="link-btn" data-act="theme"></button></div>
  </div>
</aside>
<main id="main" class="main">
  <header class="mhead"><button class="icon-btn menu-btn" data-act="menu" aria-label="Menu" aria-controls="sidebar" aria-expanded="false">${ICON.menu}</button><h1 id="view-title">Today</h1><form id="search-form" class="search" role="search" autocomplete="off"><label class="sr" for="search-input">Search</label><input id="search-input" type="search" enterkeyhint="search" placeholder="Search"></form><button id="select-btn" class="btn btn-small btn-ghost" data-act="select-mode" hidden>${ICON.select}Select</button><button class="sync-pill" data-act="sync" aria-controls="syncpop" aria-haspopup="dialog"></button></header>
  <form id="qa-form" class="qa" autocomplete="off">
    <div class="qa-head"><h2>New task</h2><button type="button" class="icon-btn" data-act="qa-close" aria-label="Close">${ICON.close}</button></div>
    <div class="qa-bar">
      <div class="qa-field"><div id="qa-backdrop" class="qa-backdrop" aria-hidden="true"></div><input id="qa-input" type="text" enterkeyhint="done" aria-label="New task" aria-describedby="qa-chips" placeholder="Add a task: Water the plants tomorrow 9am #garden"></div>
      <div class="qa-meta"><label class="sr" for="qa-list">List</label><select id="qa-list" class="qa-pill"></select><span id="qa-agent" class="qa-agent" hidden></span><button id="qa-add" class="btn btn-primary" type="submit">${ICON.plus}Add</button></div>
    </div>
    <div class="qa-sub"><span id="qa-chips" class="chips" aria-live="polite"></span><span id="qa-agent-why" class="muted small" hidden></span></div>
  </form>
  <div id="dayload" class="dayload" hidden></div>
  <div id="setup-banner" class="setup-banner" role="region" aria-label="Setup" hidden></div>
  <div id="tagbar" class="tagbar" role="status" hidden></div>
  <div id="viewswitch" class="viewswitch" hidden></div>
  <div id="list" class="list"></div>
  <div id="bulkbar" class="bulkbar" role="toolbar" aria-label="Selected tasks" hidden></div>
  <div id="settings" class="settings" hidden></div>
  <div id="wizard" class="settings wizard-view" hidden></div>
</main>
<aside id="detail" class="detail" aria-label="Task details"></aside>
</div>
<div id="scrim" class="scrim" hidden></div>
<button class="fab" data-act="new" aria-label="New task">${ICON.plus}</button>
<nav id="tabs" class="tabbar" aria-label="Tabs"></nav>
<div id="syncpop" class="popover" role="dialog" aria-labelledby="syncpop-h" hidden></div>
<div id="snooze" class="dialog-wrap" hidden></div>
<div id="move" class="dialog-wrap" hidden></div>
<div id="toast" class="toast-region" aria-live="polite" aria-atomic="true"></div>`;
}

// ---------- login ----------

function loginHTML(): string {
  return `<main class="login">
<form class="login-card" data-form="login" aria-labelledby="login-h">
  <h1 id="login-h">Meat Mode</h1>
  <p class="muted">Enter the password to open your list.</p>
  <input class="sr" type="text" name="username" autocomplete="username" value="meat-mode" tabindex="-1" aria-hidden="true">
  <label for="login-password">Password</label>
  <input id="login-password" name="password" type="password" autocomplete="current-password" required${S.loginError ? ' aria-describedby="login-error"' : ""}>
  ${S.loginError ? `<p id="login-error" class="login-error" role="alert">${esc(S.loginError)}</p>` : ""}
  <button class="btn btn-primary" type="submit"${S.loggingIn ? " disabled" : ""}>Log in</button>
</form>
</main>`;
}

/** Stops everything that talks to the hub, for the login or claim screen that replaces the app. */
function leaveShell() {
  shellUp = false;
  clearTimeout(esTimer);
  es?.close();
  es = null;
  clearTimeout(toastTimer);
  S.toast = null;
  S.state = null;
  S.open = null;
  S.selected = null;
  S.snoozeKeys = null;
  S.move = null;
  S.query = "";
  S.qaOpen = false;
  S.syncOpen = false;
  leaveSettings();
  S.loggingIn = false;
  $("#app").className = "app";
}

/** Replaces the app with the login screen. */
function showLogin() {
  if (!shellUp && $("#app").querySelector(".login:not(.claim)")) return;
  leaveShell();
  S.auth = { login: true, authenticated: false };
  $("#app").innerHTML = loginHTML();
  $<HTMLInputElement>("#login-password").focus();
}

/**
 * The claim step, alone: a hub with no password yet serves nothing else
 * (docs/api.md, Login). Setting the password logs this browser in and opens
 * the app.
 */
function claimScreenHTML(): string {
  return `<main class="login claim">
<div class="login-card claim-card" role="region" aria-labelledby="claim-h">
  <h1 id="claim-h">Meat Mode</h1>
  <p>This hub has no password yet. Set one with the hub's setup code to open your list.</p>
  ${claimHTML("claim")}
</div>
</main>`;
}

/** Draws the claim screen again, keeping what was typed. */
function drawClaim() {
  const app = $("#app");
  const kept = new Map<string, string>();
  for (const f of app.querySelectorAll<HTMLInputElement>(".claim input[id]")) kept.set(f.id, f.value);
  app.innerHTML = claimScreenHTML();
  for (const [id, v] of kept) {
    const f = document.getElementById(id) as HTMLInputElement | null;
    if (f) f.value = v;
  }
}

function showClaim() {
  if (!shellUp && $("#app").querySelector(".claim")) return;
  leaveShell();
  S.auth = { login: false, authenticated: false };
  drawClaim();
  $<HTMLInputElement>("#claim-pw-code").focus();
}

async function submitClaim(form: HTMLFormElement) {
  if (!(await claimScreen(form, drawClaim))) return;
  S.auth = { login: true, authenticated: true };
  startApp();
}

/** Shows what the session calls for instead of the app: the claim step or the login screen. */
function showGate(sess: Session): boolean {
  const g = gate(sess);
  if (g === "claim") showClaim();
  else if (g === "login") showLogin();
  return g !== null;
}

/** A 401 means the session ended, or the hub lost its password; ask which. */
function sessionEnded() {
  void api.session().then(
    (sess) => void showGate(sess.login ? { ...sess, authenticated: false } : sess),
    () => showLogin(),
  );
}

async function submitLogin(form: HTMLFormElement) {
  if (S.loggingIn) return;
  const password = form.querySelector<HTMLInputElement>("input[name=password]")!.value;
  S.loggingIn = true;
  try {
    await api.login(password);
    S.loginError = "";
    S.auth = { login: true, authenticated: true };
    S.loggingIn = false;
    startApp();
    return;
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      // This hub has no password yet: it serves only its claim step.
      S.loginError = "";
      S.loggingIn = false;
      showClaim();
      return;
    }
    S.loginError = loginErrorText(e);
  }
  S.loggingIn = false;
  $("#app").innerHTML = loginHTML();
  $<HTMLInputElement>("#login-password").focus();
}

async function logout() {
  try {
    await api.logout();
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 401)) {
      showToast(`Could not log out: ${errorText(e)}`);
      return;
    }
  }
  S.loginError = "";
  history.replaceState(null, "", location.pathname);
  showLogin();
}

// ---------- boot ----------

/** Puts the app's shell on screen and loads it. Runs on boot and after each login. */
function startApp() {
  const app = $("#app");
  app.innerHTML = shell();
  shellUp = true;
  esRetry = 0;
  $("#qa-input").addEventListener("scroll", () => ($("#qa-backdrop").scrollLeft = $<HTMLInputElement>("#qa-input").scrollLeft));
  setupSwipe($("#list"));
  setupMonthDrag($("#list"));
  setupLongPress($("#list"));

  const params = new URL(location.href).searchParams;
  const want = params.get("item");
  // A push's Reply opens the item at its note box.
  const reply = params.get("reply") === "1";
  render();
  void refresh().then(() => {
    if (want && byKey(want)) {
      const it = byKey(want)!;
      if (!isOpen(it)) S.view = "all";
      openItem(want);
      if (reply) document.querySelector<HTMLTextAreaElement>("#note-input")?.focus();
    }
  });
  connectEvents();
  // In the Android app the page asks the app about reminders, so learn that before the setup rules read them.
  void initPush().then(() => {
    resubscribeOnOpen();
    render();
    return initSetup(ctx);
  });
}

function boot() {
  const app = $("#app");
  applyTheme(getTheme());
  document.addEventListener("click", onClick);
  document.addEventListener("keydown", onKey);
  document.addEventListener("input", onInput);
  document.addEventListener("change", onChange);
  document.addEventListener("submit", onSubmit);
  document.addEventListener("focusout", onFocusOut);
  new ResizeObserver(() => render()).observe(app);
  trackKeyboard();
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") void refresh();
  });
  setInterval(render, 30000);
  setUnauthorizedHandler(sessionEnded);

  // Ask whether this hub has a password and this browser is logged in
  // before loading anything else. If the hub cannot be reached, load the app
  // anyway: it shows the error.
  void api.session().then(
    (sess) => {
      S.auth = sess;
      if (!showGate(sess)) startApp();
    },
    (e) => {
      if (!(e instanceof ApiError && e.status === 401)) startApp();
    },
  );
}

boot();
