// Meat Mode app: state, rendering and event handling. No framework: each region
// is a render function returning an HTML string, written only when it changed,
// and one delegated handler per event type.

import { ApiError, api, errorText, newId, setUnauthorizedHandler } from "./api.ts";
import { MAX_REPEAT_INTERVAL, type Action, type Check, type Comment, type Item, type NewAction, type Payload, type Repo, type RunState, type Session, type State } from "./types.ts";
import {
  DAY_CAPACITY,
  DISMISSED,
  agentNewCount,
  attention,
  buildView,
  dateChip,
  HUB_BRIDGE,
  isAgentItem,
  isOpen,
  isUnreadComment,
  openToday,
  pastDue,
  personalRepoKeys,
  repoColours,
  repoDetail,
  repoKey,
  repoLabel,
  syncStatus,
  tomorrowMorning,
  visibleRepos,
  appAuthor,
  type Group,
  type ViewId,
} from "./model.ts";
import { fmtAgo, fmtWhen, fmtWhenAt, toLocalInput, ymd } from "./dates.ts";
import { completedGroups, deletedGroups, isDeleted } from "./history.ts";
import { searchGroups } from "./search.ts";
import { bulkDismissDone, bulkDismissLabel, bulkToast, canSelect, keepPickable, pickableKeys } from "./bulk.ts";
import { snoozeOptions, typedSnooze } from "./snooze.ts";
import { parseQuickAdd, type QuickAdd } from "./quickadd.ts";
import { repeatLabel } from "./repeat.ts";
import { leadSelect, loadLead } from "./lead.ts";
import { browserZone } from "./profile.ts";
import {
  CHECKLIST_MAX,
  CHECK_MAX,
  TEXT_MAX,
  TITLE_MAX,
  addCheck,
  applyEdit,
  canEdit,
  checkText,
  checklistChip,
  diffEdit,
  draftOf,
  dueLabel,
  dueOutcome,
  editError,
  priorityChip,
  priorityOptions,
  removeCheck,
  restoreComment,
  toggleCheck,
  undoOf,
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
import { extAct, extChange, extSubmit } from "./ext.ts";
import { trackKeyboard } from "./keyboard.ts";
import { loadStatusIfStale, publicDnsWarningHTML } from "./connection.ts";
import { leaveSettings, sectionsHTML } from "./settings/registry.ts";
import { bannerHTML, initSetup, reloadSetup, setupInfo, wizardHTML, wizardShown } from "./wizard/wizard.ts";

/** Completed and Deleted show the closed items the hub keeps for 7 days. */
type View = ViewId | "completed" | "deleted" | "settings" | "setup";

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

const UNDO_MS = 6000;
const SWIPE_THRESHOLD = 88;
const PHONE_MAX = 880;

const S = {
  state: null as State | null,
  offset: 0,
  loadError: "",
  view: "today" as View,
  /** The search field's text; while it holds a word, the list shows search results in place of the view. */
  query: "",
  /** Select mode: rows pick instead of open, and the bulk bar acts on the picks. */
  selecting: false,
  picked: new Set<string>(),
  selected: null as string | null,
  open: null as string | null,
  openSeenAt: null as string | null | undefined,
  drafts: new Map<string, string>(),
  /** The open edit form of a personal item. */
  edit: null as { key: string; draft: EditDraft; error: string } | null,
  /** The note being edited, on a personal item. */
  noteEdit: null as { key: string; commentId: string; text: string } | null,
  /** Unsent text of each personal item's add-to-checklist field. */
  checkDrafts: new Map<string, string>(),
  snoozeKeys: null as string[] | null,
  syncOpen: false,
  qaOpen: false,
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
function isPhone(): boolean {
  return ($("#app").clientWidth || window.innerWidth) < PHONE_MAX;
}

const cache = new WeakMap<Element, string>();
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
  el.innerHTML = html;
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
      return "Upcoming";
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

function currentGroups(): Group[] {
  if (isPage(S.view)) return [];
  if (S.query.trim()) return searchGroups(items(), repos(), S.query, now());
  if (S.view === "completed") return completedGroups(items(), repos(), now());
  if (S.view === "deleted") return deletedGroups(items(), repos(), now());
  return buildView(S.view, items(), repos(), now());
}

/** Keys of the rows in the current view, in display order. */
function visibleKeys(): string[] {
  return currentGroups().flatMap((g) => g.items.map((i) => i.key));
}

// ---------- render: chrome ----------

/** Views under the phone's Browse tab. */
function browsing(v: View): boolean {
  return v === "all" || v === "completed" || v === "deleted" || v === "settings" || v.startsWith("list:");
}

function renderNav() {
  const all = items();
  const n = now();
  const agentNew = agentNewCount(all, repos(), user());
  // While any agent works, its tab icon carries the same light as the run's row.
  const working = workingItems(all).length;
  const agentsIcon = working ? `<span class="tab-ic" data-run="working">${TRACER}${ICON.agents}</span>` : ICON.agents;
  const agentsWorking = working ? `<span class="sr">, ${working === 1 ? "an agent is" : `${working} agents are`} working</span>` : "";
  const upcoming = buildView("upcoming", all, repos(), n).reduce((s, g) => s + g.items.length, 0);
  const everything = all.filter(isOpen).length;
  const entry = (v: View, label: string, icon: string, count: string) =>
    `<button class="nav-item" data-act="view" data-view="${esc(v)}"${S.view === v ? ' aria-current="page"' : ""}>${icon}<span class="nav-label">${label}</span>${count}</button>`;
  const plain = (c: number) => (c ? `<span class="count">${c}</span>` : "");
  setHTML(
    $("#nav"),
    entry("today", "Today", ICON.today, plain(openToday(all, n))) +
      entry("upcoming", "Upcoming", ICON.upcoming, plain(upcoming)) +
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
      ["upcoming", "Upcoming", ICON.upcoming, ""],
      ["agents", "Agents", agentsIcon, agentsWorking + (agentNew ? `<span class="tab-badge" aria-label="${agentNew} new">${agentNew}</span>` : "")],
      ["all", "Browse", ICON.all, ""],
    ]
      .map(([v, label, icon, badge]) => {
        const active = v === "all" ? browsing(S.view) : S.view === v;
        return `<button class="tab" data-act="view" data-view="${v}"${active ? ' aria-current="page"' : ""}>${icon}<span>${label}</span>${badge}</button>`;
      })
      .join(""),
  );
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
          const title = a.kind === "add" ? a.payload.title : a.kind === "sync" || a.kind === "agents" ? `${a.repo} on ${a.bridge}` : (item?.title ?? a.beadId ?? "");
          const state =
            a.status === "applied" ? (a.bridge === HUB_BRIDGE ? "Saved" : "Synced") : a.status === "pending" ? "Waiting" : a.status === "cancelled" ? "Cancelled" : `Failed: ${a.error ?? ""}`;
          return `<li class="act act-${esc(a.status)}"><span class="act-what"><span class="act-kind">${esc(actionLabel(a))}</span> ${esc(title)}</span><span class="act-state">${esc(state)} · ${esc(fmtAgo(new Date(a.createdAt), now()))}</span></li>`;
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
      return a.bridge === HUB_BRIDGE ? "Delete" : "Won't do";
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

function chipsFor(item: Item, opts: { list: boolean }): string {
  const n = now();
  const personal = personalRepoKeys(repos());
  const agent = isAgentItem(item, personal);
  const out: string[] = [];
  // The list comes first, so it starts at the same x on every row.
  if (opts.list) {
    const r = repos().find((x) => repoKey(x) === repoKey(item));
    const colour = repoColours(repos()).get(repoKey(item)) ?? 0;
    out.push(`<span class="chip chip-list"><span class="dot c${colour}" aria-hidden="true"></span>${esc(r ? listLabel(r) : item.repo)}</span>`);
  }
  const att = attention(item, personal, user());
  if (att === "new") out.push('<span class="chip chip-new">New</span>');
  if (att === "reply") out.push('<span class="chip chip-new">New reply</span>');
  const prio = priorityChip(item.priority);
  if (prio) out.push(`<span class="chip ${item.priority === 0 ? "chip-urgent" : "chip-high"}">${esc(prio)}</span>`);
  const dc = dateChip(item, n);
  if (dc) out.push(`<span class="chip chip-${dc.tone}">${dc.snoozed ? ICON.moon : ""}${esc(dc.text)}</span>`);
  if (item.repeat) out.push(`<span class="chip chip-repeat">${ICON.repeat}${esc(repeatLabel(item.repeat, n))}</span>`);
  if (agent && !isOpen(item) && item.closeReason === DISMISSED) out.push('<span class="chip chip-wontdo">Won\'t do</span>');
  if (agent && item.createdBy) out.push(`<span class="chip chip-agent">${esc(item.createdBy)}</span>`);
  const notes = item.comments?.length ?? 0;
  if (notes) out.push(`<span class="chip chip-notes" aria-label="${notes} ${notes === 1 ? "note" : "notes"}">${ICON.note}${notes}</span>`);
  const ck = checklistChip(item.checklist);
  if (ck) {
    const [done, all] = ck.split("/");
    out.push(`<span class="chip chip-checklist" aria-label="Checklist: ${done} of ${all} done">${ICON.check}${ck}</span>`);
  }
  return out.join("");
}

function checkButton(item: Item, agent: boolean): string {
  const open = isOpen(item);
  if (open && S.selecting) {
    const on = S.picked.has(item.key);
    return `<button class="check pick${agent ? " check-agent" : ""}${on ? " checked" : ""}" role="checkbox" aria-checked="${on}" data-act="pick" data-key="${esc(item.key)}" aria-label="${esc(`Select: ${item.title}`)}">${ICON.check}</button>`;
  }
  const gone = isDeleted(item, personalRepoKeys(repos()));
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
  return `<span class="run-line" data-run="${line.state}"><b>${esc(line.lead)}</b>${line.text ? ` · ${esc(line.text)}` : ""}</span>`;
}

/** The surface attributes of an element that shows a run's state: data-run, and the flash while a run has just ended. */
function runAttrs(item: Item): string {
  if (!item.run) return "";
  return ` data-run="${item.run.state}"${arrived.has(item.key) ? " data-arrive" : ""}`;
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

function rowHTML(item: Item, showList: boolean): string {
  const personal = personalRepoKeys(repos());
  const agent = isAgentItem(item, personal);
  const done = !isOpen(item);
  const picked = !done && S.selecting && S.picked.has(item.key);
  const run = agent && item.run ? item.run : null;
  return `<li class="row${done ? " is-done" : ""}${picked ? " is-picked" : ""}${item.pending ? " is-pending" : ""}${run ? " has-run" : ""}"${run ? runAttrs(item) : ""} data-key="${esc(item.key)}">
<div class="under" aria-hidden="true"><span class="u-done">Done</span><span class="u-snooze">Snooze</span></div>
<div class="row-fg">${run?.state === "working" ? TRACER : ""}${checkButton(item, agent)}<button class="row-body" data-act="open" data-key="${esc(item.key)}"><span class="row-title">${esc(item.title)}</span><span class="chips">${chipsFor(item, { list: showList })}</span>${run ? runLineHTML(item, false) : ""}${statusLine(item)}</button>${
    done
      ? isDeleted(item, personal)
        ? `<button class="btn btn-small" data-act="restore" data-key="${esc(item.key)}">${ICON.undo}Restore</button>`
        : `<button class="btn btn-small" data-act="reopen" data-key="${esc(item.key)}">${ICON.undo}Reopen</button>`
      : ""
  }</div></li>`;
}

function renderMain() {
  const title = searching() ? "Search" : viewTitle(S.view);
  setHTML($("#view-title"), esc(title));
  document.title = `${title} · Meat Mode`;
  // Search covers the task views, not Settings or setup.
  $("#search-form").hidden = isPage(S.view);
  const main = $("#main");
  main.dataset.view = S.view.startsWith("list:") ? "list" : S.view;

  // Browse chips (phone): Everything, each list, Settings.
  const colours = repoColours(repos());
  setHTML(
    $("#browse"),
    browsing(S.view)
      ? `<button class="bchip" data-act="view" data-view="all"${S.view === "all" ? ' aria-current="page"' : ""}>Everything</button>` +
          visibleRepos(repos())
            .map((r) => {
              const v = `list:${repoKey(r)}`;
              return `<button class="bchip" data-act="view" data-view="${esc(v)}"${S.view === v ? ' aria-current="page"' : ""}><span class="dot c${colours.get(repoKey(r)) ?? 0}" aria-hidden="true"></span>${esc(listLabel(r))}</button>`;
            })
            .join("") +
          `<button class="bchip" data-act="view" data-view="completed"${S.view === "completed" ? ' aria-current="page"' : ""}>${ICON.check}Completed</button>` +
          `<button class="bchip" data-act="view" data-view="deleted"${S.view === "deleted" ? ' aria-current="page"' : ""}>${ICON.trash}Deleted</button>` +
          `<button class="bchip" data-act="view" data-view="settings"${S.view === "settings" ? ' aria-current="page"' : ""}>${ICON.gear}Settings</button>`
      : "",
  );

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
  const groups = currentGroups();
  const showList = !S.view.startsWith("list:");
  if (!groups.length) {
    const none = searching()
      ? "No matches."
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
          g.id === "pastdue" && g.items.length > 1
            ? `<button class="btn btn-small btn-ghost" data-act="snooze-all">${ICON.moon}Snooze all to tomorrow</button>`
            : "";
        return `<section class="group group-${g.id}" aria-labelledby="g-${g.id}"><div class="group-head"><h2 id="g-${g.id}">${esc(g.label)} <span class="group-count">${g.items.length}</span></h2>${extra}</div><ul class="rows">${g.items
          .map((i) => rowHTML(i, showList))
          .join("")}</ul></section>`;
      })
      .join("") + (S.view === "deleted" && !searching() ? '<p class="history-note">Deleted items are removed 7 days after you delete them.</p>' : ""),
  );
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
  const label = bulkDismissLabel(picks.length ? picks : pickable.map(byKey).filter((i): i is Item => Boolean(i)), repos());
  const off = picks.length ? "" : " disabled";
  setHTML(
    bar,
    `<span class="bulk-count" aria-live="polite">${picks.length} selected</span><div class="bulk-acts"><button class="btn btn-small" data-act="bulk-done"${off}>${ICON.check}Done</button><button class="btn btn-small" data-act="bulk-snooze"${off}>${ICON.moon}Snooze…</button><button class="btn btn-small btn-danger" data-act="bulk-dismiss"${off}>${ICON.trash}${esc(label)}</button></div><div class="bulk-more"><button class="btn btn-small btn-ghost" data-act="bulk-all">Select all</button><button class="btn btn-small btn-ghost" data-act="bulk-cancel">Cancel</button></div>`,
  );
}

// ---------- render: detail ----------

function snoozeBlock(prefix: string): string {
  const n = now();
  return `<div class="snooze-grid">${snoozeOptions(n)
    .map(
      (o) =>
        `<button class="snz" data-act="snooze-opt" data-until="${o.until.toISOString()}"><span class="snz-label">${esc(o.label)}</span><span class="snz-hint">${esc(o.hint)}</span></button>`,
    )
    .join("")}</div><div class="snooze-pick"><label for="${prefix}-pick">Pick a time</label><div class="pick-row"><input id="${prefix}-pick" type="datetime-local" min="${toLocalInput(n)}" data-pick><button class="btn" data-act="snooze-pick">Snooze</button></div></div>${snoozeTypeForm(prefix)}`;
}

/** Text typed in a snooze time box, by the box's id, so its line survives a redraw. */
const snoozeTyped = new Map<string, string>();

function snoozeTypeForm(prefix: string): string {
  const id = `${prefix}-type`;
  const { until, line } = typedSnooze(snoozeTyped.get(id) ?? "", now());
  const invalid = !until && line ? ` aria-invalid="true"` : "";
  return `<form class="snooze-pick" data-form="snooze-type"><label for="${id}">Type a time</label><div class="pick-row"><input id="${id}" type="text" autocomplete="off" enterkeyhint="go" placeholder="8pm, in 2 hours, tomorrow morning" aria-describedby="${id}-now"${invalid} data-type-time><button type="submit" class="btn" aria-label="Snooze to the typed time">Snooze</button></div><p id="${id}-now" class="snooze-type-now" aria-live="polite">${esc(line)}</p></form>`;
}

/** Updates the line under a snooze time box, and only that, while typing. */
function updateSnoozeType(input: HTMLInputElement) {
  snoozeTyped.set(input.id, input.value);
  const { until, line } = typedSnooze(input.value, now());
  const out = document.getElementById(`${input.id}-now`);
  if (out) out.textContent = line;
  if (!until && line) input.setAttribute("aria-invalid", "true");
  else input.removeAttribute("aria-invalid");
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
}

/** The edit form of a personal item: title, due, details and priority. Values are set after the HTML is written. */
function editFormHTML(item: Item): string {
  const e = S.edit!;
  const d = e.draft;
  const prio = priorityOptions(item.priority)
    .map((o) => `<option value="${o.value}"${o.value === d.priority ? " selected" : ""}>${esc(o.label)}</option>`)
    .join("");
  return `<form class="edit-form" data-form="edit" aria-labelledby="d-edit-h" novalidate>
<h3 id="d-edit-h" class="sr">Edit task</h3>
<label for="edit-title">Title</label><input id="edit-title" type="text" maxlength="${TITLE_MAX}" autocomplete="off" required>
<label for="edit-due">Due</label><div class="pick-row"><input id="edit-due" type="text" autocomplete="off" enterkeyhint="done" placeholder="tomorrow 9am" aria-describedby="edit-due-now"><button type="button" class="btn" data-act="edit-clear-due">Clear</button></div><p id="edit-due-now" class="edit-due-now" aria-live="polite">${esc(dueText(item, d))}</p>
${repeatFieldsHTML(item, d)}
<label for="edit-lead">Remind me early</label>${leadSelect("edit-lead", d.remindBefore, true)}
<label for="edit-desc">Details</label><textarea id="edit-desc" rows="4" maxlength="${TEXT_MAX}"></textarea>
<label for="edit-prio">Priority</label><select id="edit-prio">${prio}</select>
${e.error ? `<p class="row-error" role="alert">${esc(e.error)}</p>` : ""}
<div class="btn-row"><button type="submit" class="btn btn-primary">${ICON.check}Save</button><button type="button" class="btn" data-act="edit-cancel">Cancel</button></div>
</form>`;
}

const REPEAT_CHOICES: [EditDraft["repeat"], string][] = [
  ["", "Does not repeat"],
  ["day", "Daily"],
  ["week", "Weekly"],
  ["month", "Monthly"],
  ["year", "Yearly"],
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

/** The edit form's repeat: how often, a weekly rule's days, whether to count from done, and its end date. */
function repeatFieldsHTML(item: Item, d: EditDraft): string {
  const pick = REPEAT_CHOICES.map(([v, label]) => `<option value="${v}"${v === d.repeat ? " selected" : ""}>${esc(label)}</option>`).join("");
  const select = `<label for="edit-repeat">Repeat</label><select id="edit-repeat">${pick}</select>`;
  if (!d.repeat) return select;
  const days =
    d.repeat === "week" && !d.fromDone
      ? `<fieldset class="weekdays" aria-describedby="edit-wd-hint"><legend>On</legend>${WEEK.map(
          ([n, name]) => `<label class="wd"><input type="checkbox" id="edit-wd-${n}" data-weekday="${n}"${d.weekdays.includes(n) ? " checked" : ""}>${name}</label>`,
        ).join("")}</fieldset><p class="muted small" id="edit-wd-hint">With none picked, it repeats on the due date's day.</p>`
      : "";
  return `${select}<div class="repeat-opts"><label for="edit-interval">Every</label><div class="pick-row"><input id="edit-interval" type="number" min="1" max="${MAX_REPEAT_INTERVAL}" step="1" inputmode="numeric"><span id="edit-unit">${unitText(d)}</span></div>${days}<label class="check-row"><input type="checkbox" id="edit-from-done"${d.fromDone ? " checked" : ""}>Count the next date from when it is done</label>${untilFieldHTML(item, d)}</div>`;
}

/** A repeat's optional end date; empty, it repeats for good. */
function untilFieldHTML(item: Item, d: EditDraft): string {
  const due = dueOutcome(item, d, now());
  const min = due.kind === "set" || (due.kind === "keep" && due.at) ? ` min="${ymd(due.at!)}"` : "";
  return `<label for="edit-until">Ends</label><div class="pick-row until-row"><input id="edit-until" type="date"${min} aria-describedby="edit-until-hint"><button type="button" class="btn" data-act="edit-clear-until">No end date</button></div><p class="muted small" id="edit-until-hint">The last date it may land on. With none, it repeats for good.</p>`;
}

/** What the due field will save, in words. */
function dueText(item: Item, d: EditDraft): string {
  const due = dueOutcome(item, d, now());
  switch (due.kind) {
    case "invalid":
      return "Could not read that date.";
    case "clear":
      return "No due date";
    default:
      return due.at ? dueLabel(due.at, now()) : "No due date";
  }
}

function checklistHTML(item: Item): string {
  const list = item.checklist ?? [];
  const rows = list
    .map(
      (c, i) =>
        `<li class="ck${c.done ? " ck-done" : ""}"><input type="checkbox" id="ck-${i}" data-check="${i}"${c.done ? " checked" : ""}><label for="ck-${i}">${esc(c.text)}</label><button class="link-btn ck-remove" data-act="check-remove" data-idx="${i}" aria-label="Remove: ${esc(c.text)}" title="Remove">${ICON.close}</button></li>`,
    )
    .join("");
  const count = checklistChip(list);
  return `<section class="d-section" aria-labelledby="d-check"><h3 id="d-check">Checklist${count ? ` <span class="group-count">${count}</span>` : ""}</h3>${
    rows ? `<ul class="checklist">${rows}</ul>` : ""
  }<form class="pick-row" data-form="check-add"><label class="sr" for="check-input">Add to the checklist</label><input id="check-input" type="text" maxlength="${CHECK_MAX}" autocomplete="off" enterkeyhint="done" placeholder="Add an entry"><button type="submit" class="btn">${ICON.plus}Add</button></form></section>`;
}

/**
 * An agent item's card in the sheet: why the agent asks, the run's line in
 * its state's colour, and the one button that fits the state. Ask agent and
 * Retry wake the agent; Stop ends a live run. Where agents are off for the
 * list, the card says so and gives the command that switches them on at the
 * list's machine, since the app cannot.
 */
function runCardHTML(item: Item, allowed: boolean): string {
  const who = agentName(item);
  const run = item.run;
  const key = esc(item.key);
  const label = item.description ? `${esc(who)} asks because` : esc(who);
  const reason = item.description ? `<p>${esc(item.description)}</p>` : "";
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
  const off = allowed
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

function noteHTML(item: Item, c: Comment, editable: boolean, queued = false): string {
  const own = appAuthor(user());
  const mine = c.author === own;
  const unread = isUnreadComment(c.createdAt, c.author, S.openSeenAt, user());
  const editing = editable && S.noteEdit?.key === item.key && S.noteEdit.commentId === c.id;
  const body = editing
    ? `<form class="note-edit" data-form="note-edit"><label class="sr" for="note-edit-input">Edit note</label><textarea id="note-edit-input" rows="3" maxlength="${TEXT_MAX}"></textarea><div class="btn-row"><button type="submit" class="btn btn-small btn-primary">Save</button><button type="button" class="btn btn-small" data-act="note-edit-cancel">Cancel</button></div></form>`
    : `<p>${esc(c.text)}</p>`;
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
  const personal = personalRepoKeys(repos());
  const agent = isAgentItem(item, personal);
  const editable = !agent && canEdit(item);
  const editing = editable && S.edit?.key === item.key;
  const open = isOpen(item);
  const allowed = agent && agentsAllowed(item, repos());
  const why = agent
    ? runCardHTML(item, allowed)
    : item.description
      ? `<div class="why"><div class="why-label">Details</div><p>${esc(item.description)}</p></div>`
      : "";
  const queued = queuedComments(item, user());
  const comments = (item.comments ?? []).map((c) => noteHTML(item, c, editable, queued.has(c.id))).join("");
  const actions = `<div class="d-actions">${
    open
      ? `<button class="btn btn-primary" data-act="done" data-key="${esc(item.key)}">${ICON.check}Done</button>${
          editable ? `<button class="btn" id="d-edit" data-act="edit" data-key="${esc(item.key)}">${ICON.edit}Edit</button>` : ""
        }<button class="btn" data-act="dismiss" data-key="${esc(item.key)}">${agent ? `${ICON.ban}Won't do` : `${ICON.trash}Delete`}</button>`
      : isDeleted(item, personalRepoKeys(repos()))
        ? `<button class="btn btn-primary" data-act="restore" data-key="${esc(item.key)}">${ICON.undo}Restore</button>`
        : `<button class="btn btn-primary" data-act="reopen" data-key="${esc(item.key)}">${ICON.undo}Reopen</button>${
          editable ? `<button class="btn" id="d-edit" data-act="edit" data-key="${esc(item.key)}">${ICON.edit}Edit</button>` : ""
        }`
  }</div>`;
  const html = `<div class="detail-inner" role="region" aria-labelledby="d-title">
<div class="grip" aria-hidden="true"></div>
<div class="d-head">${checkButton(item, agent)}<h2 id="d-title" class="d-title">${esc(item.title)}</h2><button class="icon-btn" data-act="close-detail" aria-label="Close details">${ICON.close}</button></div>
<div class="chips d-chips">${chipsFor(item, { list: true })}<code class="bead-id">${esc(item.id)}</code></div>
${statusLine(item) ? `<p class="d-status">${statusLine(item)}</p>` : ""}
${editing ? editFormHTML(item) : why + actions}
${editable ? checklistHTML(item) : ""}
${open ? `<section class="d-section" aria-labelledby="d-snz"><h3 id="d-snz">Snooze</h3>${snoozeBlock("d")}</section>` : ""}
<section class="d-section" aria-labelledby="d-notes"><h3 id="d-notes">Notes and replies</h3>${comments ? `<ol class="notes">${comments}</ol>` : '<p class="muted small">No notes yet.</p>'}
<label for="note-input" class="note-label">Add a note</label><textarea id="note-input" rows="3" maxlength="${TEXT_MAX}"></textarea>${composeHTML(item, allowed)}</section>
</div>`;
  if (setHTML(el, html)) {
    const ta = $<HTMLTextAreaElement>("#note-input", el);
    ta.value = S.drafts.get(item.key) ?? "";
    const ck = el.querySelector<HTMLInputElement>("#check-input");
    if (ck) ck.value = S.checkDrafts.get(item.key) ?? "";
    if (editing) {
      const d = S.edit!.draft;
      setValue("#edit-title", d.title);
      setValue("#edit-due", d.dueText);
      setValue("#edit-desc", d.description);
      setValue("#edit-interval", d.interval);
      setValue("#edit-until", d.until);
    }
    if (S.noteEdit?.key === item.key) setValue("#note-edit-input", S.noteEdit.text);
  }
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
  toast: (text) => showToast(text),
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
  renderToast();
  renderTheme();
  applySelection();
  renderBulk();
  $("#scrim").hidden = !(S.snoozeKeys || (isPhone() && (S.open || S.qaOpen)));
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

function patchLocal(key: string, fn: (i: Item) => void) {
  const it = byKey(key);
  if (!it) return;
  fn(it);
  // The hub applies actions on the personal list at once; nothing waits.
  it.pending = it.bridge !== HUB_BRIDGE;
  it.error = undefined;
}

/** Posts an action; the hub's copy of it, or null when it failed (a toast says why). */
async function post(a: NewAction): Promise<Action | null> {
  try {
    return await api.postAction(a);
  } catch (e) {
    showToast(`Could not save: ${(e as Error).message}`);
    return null;
  }
}

function inverse(a: NewAction): NewAction | null {
  switch (a.kind) {
    case "done":
    case "dismiss":
      return { id: newId(), kind: "reopen", key: a.key, payload: {} };
    case "reopen":
      return { id: newId(), kind: "dismiss", key: a.key, payload: {} };
    case "snooze":
      return { id: newId(), kind: "snooze", key: a.key, payload: { until: a.payload.previous ?? null, previous: a.payload.until ?? null } };
    default:
      return null;
  }
}

async function undoAll(posted: NewAction[]) {
  for (const a of posted) {
    let cancelled = false;
    try {
      cancelled = await api.cancelAction(a.id);
    } catch (e) {
      showToast(`Could not undo: ${(e as Error).message}`);
      return;
    }
    if (!cancelled) {
      const inv = inverse(a);
      if (inv) await post(inv);
    }
  }
  await refresh();
}

/** Post closing actions and offer Undo. */
async function closing(actions: NewAction[], text: string, tell?: string) {
  const posted: NewAction[] = [];
  for (const a of actions) if (await post(a)) posted.push(a);
  if (posted.length) showToast(text, () => undoAll(posted), tell);
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

function doDone(key: string) {
  const it = byKey(key);
  if (!it || !isOpen(it)) return;
  moveSelectionOff([key]);
  if (it.repeat && canEdit(it)) {
    void doneRepeating(it);
    return;
  }
  const t = now().toISOString();
  patchLocal(key, (i) => {
    i.status = "closed";
    i.closedAt = t;
    i.closeReason = "Done in Meat Mode";
  });
  render();
  void closing([{ id: newId(), kind: "done", key, payload: {} }], `Done: ${it.title}`, offersTell(it, repos()) ? key : undefined);
}

/**
 * Done on a repeating personal item: the hub moves it to its next date and
 * keeps it open. Its Undo is the edit the reply's `was` holds, not a reopen.
 */
async function doneRepeating(it: Item) {
  const key = it.key;
  render();
  const applied = await post({ id: newId(), kind: "done", key, payload: {} });
  await refresh();
  if (!applied) return;
  const u = undoOf(applied, key, newId());
  const next = byKey(key);
  if (!u || !next?.dueAt || !isOpen(next)) {
    // The hub closed it after all, as when its repeat ended meanwhile.
    showToast(`Done: ${it.title}`, () => undoAll([{ id: applied.id, kind: "done", key, payload: {} }]));
    return;
  }
  showToast(`Done. Next: ${fmtWhenAt(new Date(next.dueAt), now())}`, async () => {
    patchLocal(key, (i) => applyLocal(i, u));
    render();
    await post(u);
    await refresh();
  });
}

/** Won't do on an agent item; Delete on a personal one, which then shows nowhere. */
function doDismiss(key: string) {
  const it = byKey(key);
  if (!it || !isOpen(it)) return;
  const agent = isAgentItem(it, personalRepoKeys(repos()));
  moveSelectionOff([key]);
  const t = now().toISOString();
  patchLocal(key, (i) => {
    i.status = "closed";
    i.closedAt = t;
    i.closeReason = DISMISSED;
  });
  if (!agent && S.open === key) closeDetail();
  else render();
  void closing([{ id: newId(), kind: "dismiss", key, payload: {} }], `${agent ? "Won't do" : "Deleted"}: ${it.title}`, offersTell(it, repos()) ? key : undefined);
}

/** Brings a deleted personal item back; Undo deletes it again. */
async function doRestore(key: string) {
  const it = byKey(key);
  if (!it || isOpen(it)) return;
  patchLocal(key, (i) => {
    i.status = "open";
    i.closedAt = null;
    i.closeReason = undefined;
  });
  render();
  const a: NewAction = { id: newId(), kind: "reopen", key, payload: {} };
  if (await post(a)) showToast(`Restored: ${it.title}`, () => undoAll([a]));
  await refresh();
}

async function doReopen(key: string) {
  const it = byKey(key);
  if (!it || isOpen(it)) return;
  patchLocal(key, (i) => {
    i.status = "open";
    i.closedAt = null;
  });
  render();
  await post({ id: newId(), kind: "reopen", key, payload: {} });
  await refresh();
}

function doSnooze(keys: string[], until: Date) {
  const acts: NewAction[] = [];
  for (const key of keys) {
    const it = byKey(key);
    if (!it) continue;
    const payload: Payload = { until: until.toISOString(), previous: it.deferUntil ?? null };
    acts.push({ id: newId(), kind: "snooze", key, payload });
  }
  if (!acts.length) return;
  moveSelectionOff(keys);
  for (const a of acts) patchLocal(a.key!, (i) => (i.deferUntil = a.payload.until));
  S.snoozeKeys = null;
  if (keys === bulkSnoozeKeys) {
    bulkSnoozeKeys = null;
    endSelect();
  } else render();
  const label = fmtWhen(until, now());
  const text = acts.length > 1 ? `Snoozed ${acts.length} tasks to ${label}` : `Snoozed to ${label}`;
  void closing(acts, text);
}

// ---------- select mode ----------

function selectable(): boolean {
  return Boolean(S.state) && canSelect(S.view, S.query);
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
 * Done on every pick, with one Undo. Plain items close and undo through
 * closing's reopen; repeating personal items move on as doneRepeating does
 * and undo through the edit their reply's `was` holds.
 */
async function bulkDone() {
  const its = pickedItems();
  if (!its.length) return;
  const keys = its.map((i) => i.key);
  moveSelectionOff(keys);
  const repeating = its.filter((i) => i.repeat && canEdit(i));
  const plain = its.filter((i) => !repeating.includes(i));
  const t = now().toISOString();
  for (const i of plain)
    patchLocal(i.key, (x) => {
      x.status = "closed";
      x.closedAt = t;
      x.closeReason = "Done in Meat Mode";
    });
  endSelect();
  const posted: NewAction[] = [];
  for (const i of plain) {
    const a: NewAction = { id: newId(), kind: "done", key: i.key, payload: {} };
    if (await post(a)) posted.push(a);
  }
  const moved: [string, Action][] = [];
  for (const i of repeating) {
    const applied = await post({ id: newId(), kind: "done", key: i.key, payload: {} });
    if (applied) moved.push([i.key, applied]);
  }
  await refresh();
  const edits: NewAction[] = [];
  for (const [key, applied] of moved) {
    const u = undoOf(applied, key, newId());
    const next = byKey(key);
    // The hub closed it after all, as when its repeat ended meanwhile.
    if (!u || !next?.dueAt || !isOpen(next)) posted.push({ id: applied.id, kind: "done", key, payload: {} });
    else edits.push(u);
  }
  if (!posted.length && !edits.length) return;
  const done = its.filter((i) => posted.some((a) => a.key === i.key) || edits.some((a) => a.key === i.key));
  showToast(bulkToast("Done", done.map((i) => i.title)), async () => {
    for (const u of edits) patchLocal(u.key!, (i) => applyLocal(i, u));
    render();
    for (const u of edits) await post(u);
    await undoAll(posted);
  });
}

/** Delete or Won't do on every pick, with one Undo. */
function bulkDismiss() {
  const its = pickedItems();
  if (!its.length) return;
  const keys = its.map((i) => i.key);
  const verb = bulkDismissDone(its, repos());
  const personal = personalRepoKeys(repos());
  moveSelectionOff(keys);
  const t = now().toISOString();
  for (const k of keys)
    patchLocal(k, (i) => {
      i.status = "closed";
      i.closedAt = t;
      i.closeReason = DISMISSED;
    });
  const openGone = S.open && keys.includes(S.open) && !isAgentItem(byKey(S.open)!, personal);
  endSelect();
  if (openGone) closeDetail();
  void closing(
    keys.map((key) => ({ id: newId(), kind: "dismiss", key, payload: {} })),
    bulkToast(verb, its.map((i) => i.title)),
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
  if (!it) return;
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

function openEdit(key: string) {
  const it = byKey(key);
  if (!it || !canEdit(it)) return;
  S.edit = { key, draft: draftOf(it, browserZone()), error: "" };
  S.noteEdit = null;
  // The lead time choice names the hub's default.
  loadLead(ctx);
  render();
  $<HTMLInputElement>("#edit-title").focus();
}

/** Closes the edit form and puts focus back on its Edit button. */
function closeEdit() {
  S.edit = null;
  render();
  document.getElementById("d-edit")?.focus({ preventScroll: true });
}

/**
 * Posts an action on a personal item after `change` applied it locally, and
 * offers Undo with `toast` when the hub's reply says how to undo it.
 */
async function postEdit(key: string, a: NewAction, change: (i: Item) => void, toast?: string) {
  patchLocal(key, change);
  render();
  const applied = await post(a);
  if (applied && toast) {
    showToast(toast, async () => {
      const u = undoOf(applied, key, newId());
      if (!u) return;
      patchLocal(key, (i) => applyLocal(i, u));
      render();
      await post(u);
      await refresh();
    });
  }
  await refresh();
}

/** What an action on a personal item does to the local copy before the hub answers. */
function applyLocal(i: Item, a: NewAction) {
  const p = a.payload;
  switch (a.kind) {
    case "edit":
      if (p.set) applyEdit(i, p.set);
      break;
    case "note-edit":
      i.comments = (i.comments ?? []).map((c) => (c.id === p.commentId ? { ...c, text: p.text ?? "" } : c));
      break;
    case "note-delete":
      i.comments = (i.comments ?? []).filter((c) => c.id !== p.commentId);
      break;
    case "note-restore":
      if (p.comment) i.comments = restoreComment(i.comments, p.comment);
      break;
  }
}

function saveEdit() {
  const e = S.edit;
  const it = byKey(e?.key ?? null);
  if (!e || !it) return;
  const err = editError(it, e.draft, now());
  if (err) {
    e.error = err;
    render();
    const field = /title/i.test(err)
      ? "#edit-title"
      : /^Repeat every/.test(err)
        ? "#edit-interval"
        : /^End the repeat/.test(err)
          ? "#edit-until"
          : /date/i.test(err)
            ? "#edit-due"
            : "#edit-desc";
    $<HTMLElement>(field).focus();
    return;
  }
  const set = diffEdit(it, e.draft, now());
  closeEdit();
  if (!set) return;
  const a: NewAction = { id: newId(), kind: "edit", key: it.key, payload: { set } };
  void postEdit(it.key, a, (i) => applyLocal(i, a), "Saved");
}

function clearDue() {
  if (!S.edit) return;
  S.edit.draft.clearDue = true;
  S.edit.draft.dueText = "";
  // The hub ends the repeat with the due date.
  S.edit.draft.repeat = "";
  setValue("#edit-due", "");
  render();
  $<HTMLInputElement>("#edit-due").focus();
}

function openNoteEdit(key: string, commentId: string) {
  const c = byKey(key)?.comments?.find((x) => x.id === commentId);
  if (!c) return;
  S.noteEdit = { key, commentId, text: c.text };
  S.edit = null;
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
  void postEdit(n.key, a, (i) => applyLocal(i, a), "Note saved");
}

function deleteNote(key: string, commentId: string) {
  if (S.noteEdit?.commentId === commentId) S.noteEdit = null;
  const a: NewAction = { id: newId(), kind: "note-delete", key, payload: { commentId } };
  void postEdit(key, a, (i) => applyLocal(i, a), "Note deleted");
  $<HTMLElement>("#note-input").focus({ preventScroll: true });
}

/** Posts the whole new checklist; a toast with Undo only when `toast` is given. */
function setChecklist(key: string, next: Check[], toast?: string) {
  const a: NewAction = { id: newId(), kind: "edit", key, payload: { set: { checklist: next } } };
  void postEdit(key, a, (i) => applyLocal(i, a), toast);
}

function addToChecklist(key: string) {
  const it = byKey(key);
  const input = document.querySelector<HTMLInputElement>("#check-input");
  if (!it || !input) return;
  const text = checkText(input.value);
  if (!text) return;
  if ((it.checklist?.length ?? 0) >= CHECKLIST_MAX) {
    showToast(`A checklist holds up to ${CHECKLIST_MAX} entries.`);
    return;
  }
  input.value = "";
  S.checkDrafts.delete(key);
  setChecklist(key, addCheck(it.checklist, text));
}

function removeFromChecklist(key: string, index: number) {
  const it = byKey(key);
  const entry = it?.checklist?.[index];
  if (!it || !entry) return;
  setChecklist(key, removeCheck(it.checklist, index), `Removed: ${entry.text}`);
  // The button is gone; keep focus in the checklist.
  $<HTMLElement>("#check-input").focus({ preventScroll: true });
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
  S.selected = null;
  S.selecting = false;
  S.picked = new Set();
  // Picking a task view shows that view, so it ends a search; Settings keeps it for the way back.
  if (!isPage(v) && S.query) {
    S.query = "";
    $<HTMLInputElement>("#search-input").value = "";
  }
  if (isPhone()) closeDetail();
  if (v !== "settings") leaveSettings();
  // A new view sets the New task list afresh: the list being viewed, else the default.
  qaListTouched = false;
  render();
  updateQuickAdd();
  $("#list").scrollTop = 0;
  $("#wizard").scrollTop = 0;
}

function openItem(key: string) {
  const it = byKey(key);
  if (!it) return;
  if (S.open !== key) {
    S.edit = null;
    S.noteEdit = null;
  }
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
  S.edit = null;
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

let qa: QuickAdd = { title: "", due: null, repeat: null, list: null, tokens: [] };
let qaListTouched = false;
/** The New task form's agent switch as the user set it; null is its default, off, so Add wakes no agent until it is switched on. */
let qaAsk: boolean | null = null;

/** Whether Add will also wake an agent on the new task. */
function qaWakes(listKey: string): boolean {
  const r = repos().find((x) => repoKey(x) === listKey);
  return !!r && !noAgentReason(r) && (qaAsk ?? false);
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

function quickLists() {
  return visibleRepos(repos()).map((r) => ({ key: repoKey(r), name: repoLabel(r) }));
}

/** The list being viewed, else the personal repo (visibleRepos sorts it first), else the first list. */
function defaultList(): string {
  const v = visibleRepos(repos());
  const here = S.view.startsWith("list:") ? S.view.slice("list:".length) : "";
  if (here && v.some((r) => repoKey(r) === here)) return here;
  return v.length ? repoKey(v[0]) : "";
}

function updateQuickAdd() {
  if (!shellUp) return;
  const input = $<HTMLInputElement>("#qa-input");
  const select = $<HTMLSelectElement>("#qa-list");
  const opts = visibleRepos(repos())
    .map((r) => `<option value="${esc(repoKey(r))}">${esc(listLabel(r))}</option>`)
    .join("");
  const prev = select.value;
  setHTML(select, opts);
  // No lists yet (no machine paired): hide the picker rather than show it blank.
  select.hidden = !opts;
  qa = parseQuickAdd(input.value, now(), quickLists());
  if (qa.list) select.value = qa.list;
  else if (qaListTouched && prev) select.value = prev;
  else select.value = defaultList();
  // A stale pick (a list that disappeared) leaves nothing selected; fall
  // back to the default so the picker is never blank.
  if (select.selectedIndex < 0) select.value = defaultList();
  // Only the personal list repeats; on an agent's list the words stay as typed.
  if (qa.repeat && !select.value.startsWith(`${HUB_BRIDGE}/`)) qa = parseQuickAdd(input.value, now(), quickLists(), { repeat: false });

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
  setHTML($("#qa-chips"), chips.join(""));
}

async function submitQuickAdd() {
  const input = $<HTMLInputElement>("#qa-input");
  updateQuickAdd();
  const title = qa.title;
  if (!title) {
    input.focus();
    return;
  }
  const listKey = $<HTMLSelectElement>("#qa-list").value || qa.list || defaultList();
  const slash = listKey.indexOf("/");
  if (slash < 0) {
    showToast("No list to add to yet.");
    return;
  }
  const a: NewAction = {
    id: newId(),
    kind: "add",
    bridge: listKey.slice(0, slash),
    repo: listKey.slice(slash + 1),
    payload: { title, due: qa.due ? qa.due.toISOString() : null },
  };
  if (qa.early) {
    showToast("The end date is before the first date.");
    input.focus();
    return;
  }
  if (qa.repeat && qa.due) a.payload.repeat = { ...qa.repeat, timeZone: browserZone() };
  const wakes = qaWakes(listKey);
  if (wakes) a.payload.ask = true;
  if (await post(a)) {
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
  if (Date.now() - swipeEnded < 400 && (e.target as HTMLElement).closest("#list")) {
    swipeEnded = 0;
    e.preventDefault();
    e.stopPropagation();
    return;
  }
  const target = e.target as HTMLElement;
  if (target.id === "scrim") {
    if (S.snoozeKeys) closeSnooze();
    else if (S.qaOpen) closeQuickAdd();
    else closeDetail();
    return;
  }
  if (S.syncOpen && !target.closest("#syncpop") && !target.closest(".sync-pill")) {
    S.syncOpen = false;
    renderSync();
  }
  const btn = target.closest<HTMLElement>("[data-act]");
  if (!btn) return;
  const key = btn.dataset.key ?? "";
  switch (btn.dataset.act) {
    case "view":
      setView(btn.dataset.view as View);
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
    case "edit":
      openEdit(key);
      break;
    case "edit-cancel":
      closeEdit();
      break;
    case "edit-clear-due":
      clearDue();
      break;
    case "edit-clear-until":
      if (S.edit) {
        S.edit.draft.until = "";
        setValue("#edit-until", "");
        $<HTMLInputElement>("#edit-until").focus();
      }
      break;
    case "note-edit":
      if (S.open) openNoteEdit(S.open, btn.dataset.id ?? "");
      break;
    case "note-edit-cancel":
      closeNoteEdit();
      break;
    case "note-delete":
      if (S.open) deleteNote(S.open, btn.dataset.id ?? "");
      break;
    case "check-remove":
      if (S.open) removeFromChecklist(S.open, Number(btn.dataset.idx));
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
      break;
    }
    case "snooze-pick": {
      const input = btn.closest(".snooze-pick")!.querySelector<HTMLInputElement>("input[data-pick]")!;
      const until = new Date(input.value);
      if (!input.value || isNaN(until.getTime()) || until.getTime() <= now().getTime()) {
        input.setAttribute("aria-invalid", "true");
        input.focus();
        return;
      }
      input.removeAttribute("aria-invalid");
      const keys = btn.closest("#snooze") ? S.snoozeKeys ?? [] : S.open ? [S.open] : [];
      doSnooze(keys, until);
      break;
    }
    case "snooze-close":
      closeSnooze();
      break;
    case "undo": {
      const t = S.toast;
      if (t?.undo) {
        S.toast = null;
        renderToast();
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
  if (e.key === "Tab" && S.snoozeKeys) {
    // Keep focus inside the modal snooze dialog.
    const f = [...document.querySelectorAll<HTMLElement>("#snooze button, #snooze input")];
    if (!f.length) return;
    const i = f.indexOf(document.activeElement as HTMLElement);
    const next = e.shiftKey ? (i <= 0 ? f.length - 1 : i - 1) : i < 0 || i === f.length - 1 ? 0 : i + 1;
    f[next].focus();
    e.preventDefault();
    return;
  }
  if (e.key === "Escape") {
    if (S.snoozeKeys) closeSnooze();
    else if (S.syncOpen) {
      S.syncOpen = false;
      renderSync();
      $<HTMLElement>(".sync-pill:not([hidden])")?.focus();
    } else if (S.qaOpen) closeQuickAdd();
    else if (S.noteEdit && S.open) closeNoteEdit();
    else if (S.edit && S.open) closeEdit();
    else if (document.activeElement?.id === "search-input") {
      S.query = "";
      (document.activeElement as HTMLInputElement).value = "";
      (document.activeElement as HTMLElement).blur();
      render();
    } else if (isTyping(document.activeElement)) (document.activeElement as HTMLElement).blur();
    else if (S.selecting) endSelect();
    else if (S.open) closeDetail();
    else return;
    e.preventDefault();
    return;
  }
  if (e.ctrlKey || e.metaKey || e.altKey || isTyping(document.activeElement)) return;
  if (S.snoozeKeys) return;
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
        else if (it && isDeleted(it, personalRepoKeys(repos()))) void doRestore(it.key);
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
  else if (t.id === "check-input" && S.open) S.checkDrafts.set(S.open, (t as HTMLInputElement).value);
  else if (t.id === "note-edit-input" && S.noteEdit) S.noteEdit.text = (t as HTMLTextAreaElement).value;
  else if (S.edit && t.closest(".edit-form")) {
    const d = S.edit.draft;
    const v = (t as HTMLInputElement).value;
    if (t.id === "edit-title") d.title = v;
    else if (t.id === "edit-desc") d.description = v;
    else if (t.id === "edit-due") {
      d.dueText = v;
      const it = byKey(S.edit.key);
      // Only the line under the field changes while typing a date.
      if (it) $("#edit-due-now").textContent = dueText(it, d);
    } else if (t.id === "edit-interval") {
      d.interval = v;
      $("#edit-unit").textContent = unitText(d);
    } else if (t.id === "edit-until") d.until = v;
  }
}

function onChange(e: Event) {
  const t = e.target as HTMLElement;
  if (S.edit && t.closest(".edit-form")) editChanged(S.edit.draft, t as HTMLInputElement);
  else if (t.dataset.check !== undefined && S.open) {
    const it = byKey(S.open);
    if (it) setChecklist(it.key, toggleCheck(it.checklist, Number(t.dataset.check)));
  } else if (t.id === "qa-list") {
    qaListTouched = true;
    updateQaAgent((t as HTMLSelectElement).value);
  }
  else if (shellUp) extChange(e.target as HTMLElement, ctx);
}

/** A select or checkbox of the edit form changed. Fields that show or hide with it redraw. */
function editChanged(d: EditDraft, t: HTMLInputElement) {
  if (t.id === "edit-prio") d.priority = Number(t.value);
  else if (t.id === "edit-lead") d.remindBefore = t.value === "" ? null : Number(t.value);
  else if (t.id === "edit-repeat") {
    d.repeat = t.value as EditDraft["repeat"];
    render();
  } else if (t.id === "edit-from-done") {
    d.fromDone = t.checked;
    render();
  } else if (t.dataset.weekday !== undefined) {
    const n = Number(t.dataset.weekday);
    d.weekdays = t.checked ? [...d.weekdays, n] : d.weekdays.filter((x) => x !== n);
    render();
  }
}

function onSubmit(e: SubmitEvent) {
  const form = e.target as HTMLFormElement;
  e.preventDefault();
  if (form.dataset.form === "login") void submitLogin(form);
  else if (form.dataset.form === "pw-claim" && form.closest(".claim")) void submitClaim(form);
  else if (form.id === "qa-form") void submitQuickAdd();
  else if (form.id === "search-form") (document.activeElement as HTMLElement | null)?.blur();
  else if (form.dataset.form === "edit") saveEdit();
  else if (form.dataset.form === "note-edit") saveNoteEdit();
  else if (form.dataset.form === "check-add" && S.open) addToChecklist(S.open);
  else if (form.dataset.form === "snooze-type") submitSnoozeType(form);
  else if (shellUp) void extSubmit(form, ctx);
}

// Swipe on phone rows: right = done, left = snooze.
function setupSwipe(list: HTMLElement) {
  let start: { x: number; y: number; id: number; li: HTMLElement; fg: HTMLElement } | null = null;
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
    start = { x: e.clientX, y: e.clientY, id: e.pointerId, li, fg };
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
    dx = mx;
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
    reset();
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
<aside class="sidebar" aria-label="Navigation">
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
  <header class="mhead"><h1 id="view-title">Today</h1><form id="search-form" class="search" role="search" autocomplete="off"><label class="sr" for="search-input">Search</label><input id="search-input" type="search" enterkeyhint="search" placeholder="Search"></form><button id="select-btn" class="btn btn-small btn-ghost" data-act="select-mode" hidden>${ICON.select}Select</button><button class="sync-pill" data-act="sync" aria-controls="syncpop" aria-haspopup="dialog"></button></header>
  <div id="browse" class="browse"></div>
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
  <div id="list" class="list"></div>
  <div id="bulkbar" class="bulkbar" role="toolbar" aria-label="Selected tasks" hidden></div>
  <div id="settings" class="settings" hidden></div>
  <div id="wizard" class="settings wizard-view" hidden></div>
</main>
<aside id="detail" class="detail" aria-label="Task details"></aside>
</div>
<div id="scrim" class="scrim" hidden></div>
<button class="fab" data-act="new" aria-label="New task">${ICON.plus}</button>
<nav id="tabs" class="tabbar" aria-label="Views"></nav>
<div id="syncpop" class="popover" role="dialog" aria-labelledby="syncpop-h" hidden></div>
<div id="snooze" class="dialog-wrap" hidden></div>
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
