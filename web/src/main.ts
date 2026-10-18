// Meat Mode app: state, rendering and event handling. No framework: each region
// is a render function returning an HTML string, written only when it changed,
// and one delegated handler per event type.

import { ApiError, api, newId, setUnauthorizedHandler } from "./api.ts";
import type { Action, Item, NewAction, NewBridgeReply, Payload, Repo, Session, State } from "./types.ts";
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
  type ViewId,
} from "./model.ts";
import { fmtAgo, fmtTime, fmtWhen, toLocalInput } from "./dates.ts";
import { snoozeOptions } from "./snooze.ts";
import { parseQuickAdd, type QuickAdd } from "./quickadd.ts";
import { applyTheme, getTheme, setTheme, themeChoiceChanged, themeChoicesHTML } from "./theme.ts";
import { loginErrorText, needsLogin } from "./login.ts";
import { esc } from "./html.ts";
import { machineReposHTML } from "./agents.ts";
import { remindersOn, subscribePush, turnOnReminders } from "./push.ts";
import type { AppContext } from "./app-context.ts";
import { extAct, extChange, extSubmit } from "./ext.ts";
import { trackKeyboard } from "./keyboard.ts";
import { sectionsHTML } from "./settings/registry.ts";
import { bannerHTML, initSetup, reloadSetup, setupInfo, wizardHTML, wizardShown } from "./wizard/wizard.ts";

type View = ViewId | "settings" | "setup";

/** Views that show a page in place of the task list. */
function isPage(v: View): v is "settings" | "setup" {
  return v === "settings" || v === "setup";
}

interface Toast {
  id: number;
  text: string;
  undo?: () => Promise<void>;
}

const UNDO_MS = 6000;
const SWIPE_THRESHOLD = 88;
const PHONE_MAX = 880;

const S = {
  state: null as State | null,
  offset: 0,
  loadError: "",
  view: "today" as View,
  selected: null as string | null,
  open: null as string | null,
  openSeenAt: null as string | null | undefined,
  drafts: new Map<string, string>(),
  snoozeKeys: null as string[] | null,
  syncOpen: false,
  syncActions: null as Action[] | null,
  syncError: false,
  qaOpen: false,
  toast: null as Toast | null,
  newBridge: null as NewBridgeReply | null,
  bridgeError: "",
  revoking: null as string | null,
  pushStatus: "",
  copied: false,
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

// ---------- icons ----------

const ICON = {
  today: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1 7 17M17 7l2.1-2.1"/></svg>',
  upcoming: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/></svg>',
  agents: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="8" width="14" height="11" rx="3"/><path d="M12 4v4M9 13h.01M15 13h.01M2 13v2M22 13v2"/></svg>',
  all: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg>',
  moon: '<svg class="i-moon" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>',
  note: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
  plus: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>',
  close: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18"/></svg>',
  gear: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></svg>',
  sun: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.4 1.4M17.6 17.6 19 19M5 19l1.4-1.4M17.6 6.4 19 5"/></svg>',
  check: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg>',
};

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

function currentGroups() {
  if (isPage(S.view)) return [];
  return buildView(S.view, items(), repos(), now());
}

/** Keys of the rows in the current view, in display order. */
function visibleKeys(): string[] {
  return currentGroups().flatMap((g) => g.items.map((i) => i.key));
}

// ---------- render: chrome ----------

function renderNav() {
  const all = items();
  const n = now();
  const agentNew = agentNewCount(all, repos(), user());
  const upcoming = buildView("upcoming", all, repos(), n).reduce((s, g) => s + g.items.length, 0);
  const everything = all.filter(isOpen).length;
  const entry = (v: View, label: string, icon: string, count: string) =>
    `<button class="nav-item" data-act="view" data-view="${esc(v)}"${S.view === v ? ' aria-current="page"' : ""}>${icon}<span class="nav-label">${label}</span>${count}</button>`;
  const plain = (c: number) => (c ? `<span class="count">${c}</span>` : "");
  setHTML(
    $("#nav"),
    entry("today", "Today", ICON.today, plain(openToday(all, n))) +
      entry("upcoming", "Upcoming", ICON.upcoming, plain(upcoming)) +
      entry("agents", "From agents", ICON.agents, agentNew ? `<span class="badge-new">${agentNew} new</span>` : "") +
      entry("all", "Everything", ICON.all, plain(everything)),
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
      ["agents", "Agents", ICON.agents, agentNew ? `<span class="tab-badge" aria-label="${agentNew} new">${agentNew}</span>` : ""],
      ["all", "Browse", ICON.all, ""],
    ]
      .map(([v, label, icon, badge]) => {
        const active = v === "all" ? S.view === "all" || S.view === "settings" || S.view.startsWith("list:") : S.view === v;
        return `<button class="tab" data-act="view" data-view="${v}"${active ? ' aria-current="page"' : ""}>${icon}<span>${label}</span>${badge}</button>`;
      })
      .join(""),
  );
}

function renderSync() {
  if (!shellUp) return;
  const st = S.state;
  const status = st ? syncStatus(st, now()) : { tone: "orange" as const, text: S.loadError ? "Hub unreachable" : "Loading" };
  const html = `<span class="pill-dot" aria-hidden="true"></span><span>${esc(status.text)}</span>`;
  for (const el of document.querySelectorAll<HTMLElement>(".sync-pill")) {
    el.dataset.tone = status.tone;
    el.setAttribute("aria-expanded", String(S.syncOpen));
    setHTML(el, html);
  }
  const pop = $("#syncpop");
  pop.hidden = !S.syncOpen;
  if (!S.syncOpen) return;
  const list = S.syncActions;
  let body: string;
  if (S.syncError) body = '<p class="muted">Could not load actions.</p>';
  else if (!list) body = '<p class="muted">Loading</p>';
  else if (!list.length) body = '<p class="muted">No actions yet.</p>';
  else
    body =
      '<ul class="actions">' +
      list
        .map((a) => {
          const item = byKey(`${a.bridge}/${a.repo}/${a.beadId ?? ""}`);
          const title = a.kind === "add" ? a.payload.title : a.kind === "sync" ? `${a.repo} on ${a.bridge}` : (item?.title ?? a.beadId ?? "");
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
    case "snooze":
      return a.payload.until ? `Snooze to ${fmtWhen(new Date(a.payload.until), now())}:` : "Clear snooze on";
  }
}

// ---------- render: list ----------

function chipsFor(item: Item, opts: { list: boolean }): string {
  const n = now();
  const personal = personalRepoKeys(repos());
  const agent = isAgentItem(item, personal);
  const out: string[] = [];
  const att = attention(item, personal, user());
  if (att === "new") out.push('<span class="chip chip-pink">New</span>');
  if (att === "reply") out.push('<span class="chip chip-cyan">New reply</span>');
  const dc = dateChip(item, n);
  if (dc) out.push(`<span class="chip chip-${dc.tone}">${dc.snoozed ? ICON.moon : ""}${esc(dc.text)}</span>`);
  if (opts.list) {
    const r = repos().find((x) => repoKey(x) === repoKey(item));
    const colour = repoColours(repos()).get(repoKey(item)) ?? 0;
    out.push(`<span class="chip chip-list"><span class="dot c${colour}" aria-hidden="true"></span>${esc(r ? listLabel(r) : item.repo)}</span>`);
  }
  if (agent && item.createdBy) out.push(`<span class="chip chip-agent">${esc(item.createdBy)}</span>`);
  const notes = item.comments?.length ?? 0;
  if (notes) out.push(`<span class="chip chip-notes" aria-label="${notes} ${notes === 1 ? "note" : "notes"}">${ICON.note}${notes}</span>`);
  return out.join("");
}

function checkButton(item: Item, agent: boolean): string {
  const open = isOpen(item);
  const label = open ? `Mark done: ${item.title}` : `Reopen: ${item.title}`;
  return `<button class="check${agent ? " check-agent" : ""}${open ? "" : " checked"}" data-act="${open ? "done" : "reopen"}" data-key="${esc(item.key)}" aria-label="${esc(label)}">${ICON.check}</button>`;
}

function statusLine(item: Item): string {
  if (item.error) return `<span class="row-error" role="status">${esc(item.error)}</span>`;
  if (item.pending) return '<span class="row-pending">Waiting for sync</span>';
  return "";
}

function rowHTML(item: Item, showList: boolean): string {
  const personal = personalRepoKeys(repos());
  const agent = isAgentItem(item, personal);
  const done = !isOpen(item);
  return `<li class="row${done ? " is-done" : ""}${item.pending ? " is-pending" : ""}" data-key="${esc(item.key)}">
<div class="under" aria-hidden="true"><span class="u-done">Done</span><span class="u-snooze">Snooze</span></div>
<div class="row-fg">${checkButton(item, agent)}<button class="row-body" data-act="open" data-key="${esc(item.key)}"><span class="row-title">${esc(item.title)}</span><span class="chips">${chipsFor(item, { list: showList })}</span>${statusLine(item)}</button>${
    done ? `<button class="btn btn-small" data-act="reopen" data-key="${esc(item.key)}">Reopen</button>` : ""
  }</div></li>`;
}

function renderMain() {
  setHTML($("#view-title"), esc(viewTitle(S.view)));
  document.title = `${viewTitle(S.view)} · Meat Mode`;
  const main = $("#main");
  main.dataset.view = S.view.startsWith("list:") ? "list" : S.view;

  // Browse chips (phone): Everything, each list, Settings.
  const browsing = S.view === "all" || S.view === "settings" || S.view.startsWith("list:");
  const colours = repoColours(repos());
  setHTML(
    $("#browse"),
    browsing
      ? `<button class="bchip" data-act="view" data-view="all"${S.view === "all" ? ' aria-current="page"' : ""}>Everything</button>` +
          visibleRepos(repos())
            .map((r) => {
              const v = `list:${repoKey(r)}`;
              return `<button class="bchip" data-act="view" data-view="${esc(v)}"${S.view === v ? ' aria-current="page"' : ""}><span class="dot c${colours.get(repoKey(r)) ?? 0}" aria-hidden="true"></span>${esc(listLabel(r))}</button>`;
            })
            .join("") +
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
    setHTML(list, `<p class="empty">${S.view === "today" ? "Nothing due today." : "Nothing here."}</p>`);
    return;
  }
  setHTML(
    list,
    groups
      .map((g) => {
        const extra =
          g.id === "pastdue" && g.items.length > 1
            ? `<button class="btn btn-small btn-ghost" data-act="snooze-all">Snooze all to tomorrow</button>`
            : "";
        return `<section class="group group-${g.id}" aria-labelledby="g-${g.id}"><div class="group-head"><h2 id="g-${g.id}">${esc(g.label)} <span class="group-count">${g.items.length}</span></h2>${extra}</div><ul class="rows">${g.items
          .map((i) => rowHTML(i, showList))
          .join("")}</ul></section>`;
      })
      .join(""),
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

// ---------- render: detail ----------

function snoozeBlock(prefix: string): string {
  const n = now();
  return `<div class="snooze-grid">${snoozeOptions(n)
    .map(
      (o) =>
        `<button class="snz" data-act="snooze-opt" data-until="${o.until.toISOString()}"><span class="snz-label">${esc(o.label)}</span><span class="snz-hint">${esc(o.hint)}</span></button>`,
    )
    .join("")}</div><div class="snooze-pick"><label for="${prefix}-pick">Pick a time</label><div class="pick-row"><input id="${prefix}-pick" type="datetime-local" min="${toLocalInput(n)}" data-pick><button class="btn" data-act="snooze-pick">Snooze</button></div></div>`;
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
  const open = isOpen(item);
  const why = item.description
    ? `<div class="why${agent ? " why-agent" : ""}"><div class="why-label">${agent ? `${esc(item.createdBy || "The agent")} asks because` : "Details"}</div><p>${esc(item.description)}</p></div>`
    : "";
  const own = appAuthor(user());
  const comments = (item.comments ?? [])
    .map((c) => {
      const mine = c.author === own;
      const unread = isUnreadComment(c.createdAt, c.author, S.openSeenAt, user());
      return `<li class="note ${mine ? "note-own" : "note-agent"}${unread ? " note-unread" : ""}"><div class="note-head"><span class="note-author">${esc(mine ? "You" : c.author)}</span><span class="note-time">${esc(fmtWhen(new Date(c.createdAt), now()))}</span>${unread ? '<span class="chip chip-cyan">New</span>' : ""}</div><p>${esc(c.text)}</p></li>`;
    })
    .join("");
  const html = `<div class="detail-inner" role="region" aria-labelledby="d-title">
<div class="grip" aria-hidden="true"></div>
<div class="d-head">${checkButton(item, agent)}<h2 id="d-title" class="d-title">${esc(item.title)}</h2><button class="icon-btn" data-act="close-detail" aria-label="Close details">${ICON.close}</button></div>
<div class="chips d-chips">${chipsFor(item, { list: true })}<code class="bead-id">${esc(item.id)}</code></div>
${statusLine(item) ? `<p class="d-status">${statusLine(item)}</p>` : ""}
${why}
<div class="d-actions">${
    open
      ? `<button class="btn btn-primary" data-act="done" data-key="${esc(item.key)}">Done</button><button class="btn" data-act="dismiss" data-key="${esc(item.key)}">${agent ? "Won't do" : "Delete"}</button>`
      : `<button class="btn btn-primary" data-act="reopen" data-key="${esc(item.key)}">Reopen</button>`
  }</div>
${open ? `<section class="d-section" aria-labelledby="d-snz"><h3 id="d-snz">Snooze</h3>${snoozeBlock("d")}</section>` : ""}
<section class="d-section" aria-labelledby="d-notes"><h3 id="d-notes">Notes and replies</h3>${comments ? `<ol class="notes">${comments}</ol>` : '<p class="muted small">No notes yet.</p>'}
<label for="note-input" class="note-label">Add a note</label><textarea id="note-input" rows="3"></textarea><button class="btn" data-act="note" data-key="${esc(item.key)}">Save note</button></section>
</div>`;
  if (setHTML(el, html)) {
    const ta = $<HTMLTextAreaElement>("#note-input", el);
    ta.value = S.drafts.get(item.key) ?? "";
  }
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
  setHTML(
    $("#toast"),
    t ? `<div class="toast"><span>${esc(t.text)}</span>${t.undo ? '<button class="btn btn-small toast-undo" data-act="undo">Undo</button>' : ""}</div>` : "",
  );
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
  const n = now();
  const bridges = S.state?.bridges ?? [];
  const perm = typeof Notification === "undefined" ? "unsupported" : Notification.permission;
  const pushHTML =
    perm === "unsupported"
      ? '<p class="muted">This browser does not support reminders.</p>'
      : perm === "granted"
        ? '<p>Reminders are on for this device.</p><button class="btn" data-act="push">Send subscription again</button>'
        : perm === "denied"
          ? '<p class="muted">Notifications are blocked for this site in the browser settings.</p>'
          : '<button class="btn btn-primary" data-act="push">Turn on reminders</button>';
  const nb = S.newBridge;
  const cmd = nb ? `meat-mode bridge pair ${location.origin} ${nb.token}` : "";
  const machines = bridges.length
    ? `<ul class="machines">${bridges
        .map((b) => {
          const seen = b.lastSeen ? `Last seen ${fmtAgo(new Date(b.lastSeen), n)}` : "Never seen";
          const actions =
            S.revoking === b.name
              ? `<span class="confirm">Revoke ${esc(b.name)}?</span><button class="btn btn-small btn-danger" data-act="revoke-confirm" data-name="${esc(b.name)}">Revoke</button><button class="btn btn-small" data-act="revoke-cancel">Cancel</button>`
              : `<button class="btn btn-small" data-act="revoke" data-name="${esc(b.name)}">Revoke</button>`;
          return `<li class="machine"><div><div class="m-name">${esc(b.name)}</div><div class="muted small">${esc(seen)}${b.version ? ` · ${esc(b.version)}` : ""}</div></div><div class="m-actions">${actions}</div>${machineRepos(b.name)}</li>`;
        })
        .join("")}</ul>`
    : '<p class="muted">No machines yet.</p>';
  const machinesHTML = `<section class="set-section" aria-labelledby="s-mach"><h2 id="s-mach">Agent machines</h2>${machines}
<form class="add-machine" data-form="bridge"><label for="bridge-name">Machine name</label><div class="pick-row"><input id="bridge-name" name="name" required pattern="[a-z0-9-]{1,32}" placeholder="laptop" autocomplete="off"><button class="btn btn-primary" type="submit">Add machine</button></div><p class="muted small">Lowercase letters, digits and dashes.</p>${
      S.bridgeError ? `<p class="row-error" role="alert">${esc(S.bridgeError)}</p>` : ""
    }</form>
${
  nb
    ? `<div class="token-box" role="status"><p>Run this on <strong>${esc(nb.name)}</strong>. The token is shown only once.</p><code class="cmd" id="pair-cmd">${esc(cmd)}</code><button class="btn" data-act="copy">${S.copied ? "Copied" : "Copy"}</button><button class="btn" data-act="token-done">Done</button></div>`
    : ""
}</section>`;
  const own: [number, string][] = [
    [10, `<section class="set-section" aria-labelledby="s-look"><h2 id="s-look">Appearance</h2>${themeChoicesHTML("settings-theme")}</section>`],
    [
      20,
      `<section class="set-section" aria-labelledby="s-push"><h2 id="s-push">Reminders</h2>${pushHTML}${S.pushStatus ? `<p class="muted small" role="status">${esc(S.pushStatus)}</p>` : ""}</section>`,
    ],
    [
      30,
      S.auth?.login
        ? '<section class="set-section" aria-labelledby="s-acct"><h2 id="s-acct">Account</h2><p class="muted">Logging out ends this device\'s session and stops its reminders.</p><button class="btn" data-act="logout">Log out</button></section>'
        : "",
    ],
    [40, machinesHTML],
  ];
  setHTML(
    $("#settings"),
    [...own, ...sectionsHTML(ctx)]
      .sort((a, b) => a[0] - b[0])
      .map(([, html]) => html)
      .join("\n"),
  );
  renderTheme();
}

/** The repos a machine found, each with its sync switch. */
function machineRepos(bridge: string): string {
  return machineReposHTML(repos(), bridge);
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
    const h = section ? document.getElementById(section) : null;
    if (h) {
      h.setAttribute("tabindex", "-1");
      h.focus();
    }
  },
};

// ---------- render ----------

function render() {
  if (!shellUp) return;
  renderNav();
  renderSync();
  renderMain();
  renderDetail();
  renderSnoozeDialog();
  renderToast();
  renderTheme();
  applySelection();
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
      S.state = st;
      S.loadError = "";
      const server = new Date(st.serverTime).getTime();
      if (!Number.isNaN(server)) S.offset = server - Date.now();
      if (S.selected && !byKey(S.selected)) S.selected = null;
      if (S.syncOpen) void loadSyncActions();
    } catch (e) {
      S.loadError = (e as Error).message;
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

async function loadSyncActions() {
  try {
    S.syncActions = await api.actions(30);
    S.syncError = false;
  } catch {
    S.syncActions = null;
    S.syncError = true;
  }
  renderSync();
}

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
          if (needsLogin(sess)) showLogin();
          else esTimer = setTimeout(connectEvents, wait);
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

async function post(a: NewAction): Promise<boolean> {
  try {
    await api.postAction(a);
    return true;
  } catch (e) {
    showToast(`Could not save: ${(e as Error).message}`);
    return false;
  }
}

function inverse(a: NewAction): NewAction | null {
  switch (a.kind) {
    case "done":
    case "dismiss":
      return { id: newId(), kind: "reopen", key: a.key, payload: {} };
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
async function closing(actions: NewAction[], text: string) {
  const posted: NewAction[] = [];
  for (const a of actions) if (await post(a)) posted.push(a);
  if (posted.length) showToast(text, () => undoAll(posted));
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
  const t = now().toISOString();
  patchLocal(key, (i) => {
    i.status = "closed";
    i.closedAt = t;
    i.closeReason = "Done in Meat Mode";
  });
  render();
  void closing([{ id: newId(), kind: "done", key, payload: {} }], `Done: ${it.title}`);
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
  void closing([{ id: newId(), kind: "dismiss", key, payload: {} }], `${agent ? "Won't do" : "Deleted"}: ${it.title}`);
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
  render();
  const label = fmtWhen(until, now());
  const text = acts.length > 1 ? `Snoozed ${acts.length} tasks to ${label}` : `Snoozed to ${label}`;
  void closing(acts, text);
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

// ---------- toast ----------

let toastSeq = 0;
let toastTimer: ReturnType<typeof setTimeout> | undefined;
function showToast(text: string, undo?: () => Promise<void>) {
  clearTimeout(toastTimer);
  const id = ++toastSeq;
  S.toast = { id, text, undo };
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
  if (isPhone()) closeDetail();
  // The pairing token is shown once; leaving Settings ends that showing.
  if (v !== "settings") S.newBridge = null;
  render();
  $("#list").scrollTop = 0;
  $("#wizard").scrollTop = 0;
}

function openItem(key: string) {
  const it = byKey(key);
  if (!it) return;
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
  setURL(null);
  render();
  const row = document.querySelector<HTMLElement>(`#list .row[data-key="${CSS.escape(key)}"] .row-body`);
  row?.focus({ preventScroll: true });
}

function setURL(key: string | null) {
  const u = new URL(location.href);
  if (key) u.searchParams.set("item", key);
  else u.searchParams.delete("item");
  history.replaceState(null, "", u);
}

let snoozeReturn: HTMLElement | null = null;
function openSnooze(keys: string[]) {
  if (!keys.length) return;
  snoozeReturn = document.activeElement as HTMLElement | null;
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

let qa: QuickAdd = { title: "", due: null, list: null, tokens: [] };
let qaListTouched = false;

function quickLists() {
  return visibleRepos(repos()).map((r) => ({ key: repoKey(r), name: repoLabel(r) }));
}

/** The personal repo (visibleRepos sorts it first), else the first list. */
function defaultList(): string {
  const v = visibleRepos(repos());
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
  if (qa.due) {
    const when = fmtWhen(qa.due, now());
    const full = when.includes(":") ? when : `${when} ${fmtTime(qa.due)}`;
    chips.push(`<span class="chip chip-purple">Due ${esc(full)}</span>`);
  }
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
  if (await post(a)) {
    input.value = "";
    qaListTouched = false;
    showToast(`Added: ${title}`);
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

async function enablePush() {
  S.pushStatus = await turnOnReminders();
  render();
}

function resubscribeOnOpen() {
  if (!remindersOn() || !("serviceWorker" in navigator)) return;
  subscribePush().catch(() => {});
}

// ---------- settings actions ----------

async function addBridge(form: HTMLFormElement) {
  const input = form.querySelector<HTMLInputElement>("input[name=name]")!;
  const name = input.value.trim();
  if (!/^[a-z0-9-]{1,32}$/.test(name)) {
    S.bridgeError = "Use 1 to 32 lowercase letters, digits or dashes.";
    render();
    return;
  }
  try {
    S.newBridge = await api.addBridge(name);
    S.bridgeError = "";
    S.copied = false;
  } catch (e) {
    S.bridgeError = (e as Error).message;
  }
  await refresh();
}

async function revokeBridge(name: string) {
  try {
    await api.revokeBridge(name);
    if (S.newBridge?.name === name) S.newBridge = null;
  } catch (e) {
    showToast(`Could not revoke: ${(e as Error).message}`);
  }
  S.revoking = null;
  await refresh();
}

async function copyCommand() {
  const text = $("#pair-cmd").textContent ?? "";
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const range = document.createRange();
    range.selectNodeContents($("#pair-cmd"));
    const sel = getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
    document.execCommand("copy");
  }
  S.copied = true;
  render();
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
      openItem(key);
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
    case "note":
      void doNote(key);
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
    case "sync":
      S.syncOpen = !S.syncOpen;
      S.syncActions = null;
      S.syncError = false;
      renderSync();
      if (S.syncOpen) {
        void loadSyncActions();
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
    case "push":
      void enablePush();
      break;
    case "revoke":
      S.revoking = btn.dataset.name ?? null;
      render();
      break;
    case "revoke-cancel":
      S.revoking = null;
      render();
      break;
    case "revoke-confirm":
      void revokeBridge(btn.dataset.name ?? "");
      break;
    case "sync-switch":
      void switchSync(btn.dataset.bridge ?? "", btn.dataset.repo ?? "");
      break;
    case "copy":
      void copyCommand();
      break;
    case "token-done":
      S.newBridge = null;
      S.copied = false;
      render();
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
    else if (isTyping(document.activeElement)) (document.activeElement as HTMLElement).blur();
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
      if (S.selected) {
        const it = byKey(S.selected);
        if (it && isOpen(it)) doDone(it.key);
        else if (it) void doReopen(it.key);
      }
      break;
    case "s":
      if (S.selected && byKey(S.selected) && isOpen(byKey(S.selected)!)) openSnooze([S.selected]);
      break;
    case "n":
      openQuickAdd();
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
  if (t.id === "qa-input") updateQuickAdd();
  else if (t.id === "note-input" && S.open) S.drafts.set(S.open, (t as HTMLTextAreaElement).value);
}

function onChange(e: Event) {
  if ((e.target as HTMLElement).id === "qa-list") qaListTouched = true;
  else if (themeChoiceChanged(e.target as HTMLElement, "settings-theme")) renderTheme();
  else if (shellUp) extChange(e.target as HTMLElement, ctx);
}

function onSubmit(e: SubmitEvent) {
  const form = e.target as HTMLFormElement;
  e.preventDefault();
  if (form.dataset.form === "login") void submitLogin(form);
  else if (form.id === "qa-form") void submitQuickAdd();
  else if (form.dataset.form === "bridge") void addBridge(form);
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
    if (!isPhone() || (e.pointerType === "mouse" && e.button !== 0)) return;
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
  <header class="mhead"><h1 id="view-title">Today</h1><button class="sync-pill" data-act="sync" aria-controls="syncpop" aria-haspopup="dialog"></button></header>
  <div id="browse" class="browse"></div>
  <form id="qa-form" class="qa" autocomplete="off">
    <div class="qa-head"><h2>New task</h2><button type="button" class="icon-btn" data-act="qa-close" aria-label="Close">${ICON.close}</button></div>
    <div class="qa-field"><div id="qa-backdrop" class="qa-backdrop" aria-hidden="true"></div><input id="qa-input" type="text" enterkeyhint="done" aria-label="New task" aria-describedby="qa-chips" placeholder="Add a task: Water the plants tomorrow 9am #garden"></div>
    <div class="qa-meta"><span id="qa-chips" class="chips" aria-live="polite"></span><label class="sr" for="qa-list">List</label><select id="qa-list"></select><button class="btn btn-primary" type="submit">Add</button></div>
  </form>
  <div id="dayload" class="dayload" hidden></div>
  <div id="setup-banner" class="setup-banner" role="region" aria-label="Setup" hidden></div>
  <div id="list" class="list"></div>
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

/** Replaces the app with the login screen and stops everything that talks to the hub. */
function showLogin() {
  if (!shellUp && $("#app").querySelector(".login")) return;
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
  S.qaOpen = false;
  S.syncOpen = false;
  S.newBridge = null;
  S.auth = { login: true, authenticated: false };
  S.loggingIn = false;
  $("#app").className = "app";
  $("#app").innerHTML = loginHTML();
  $<HTMLInputElement>("#login-password").focus();
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
      // This hub has no login after all.
      S.loginError = "";
      S.auth = { login: false, authenticated: true };
      S.loggingIn = false;
      startApp();
      return;
    }
    S.loginError = e instanceof ApiError ? loginErrorText(e.status, e.message) : loginErrorText(0, (e as Error).message);
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
      showToast(`Could not log out: ${(e as Error).message}`);
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

  const want = new URL(location.href).searchParams.get("item");
  render();
  void refresh().then(() => {
    if (want && byKey(want)) {
      const it = byKey(want)!;
      if (!isOpen(it)) S.view = "all";
      openItem(want);
    }
  });
  connectEvents();
  resubscribeOnOpen();
  void initSetup(ctx);
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
  setUnauthorizedHandler(showLogin);

  // Ask whether this hub wants a password before loading anything else. If
  // the hub cannot be reached, load the app anyway: it shows the error.
  void api.session().then(
    (sess) => {
      S.auth = sess;
      if (needsLogin(sess)) showLogin();
      else startApp();
    },
    (e) => {
      if (!(e instanceof ApiError && e.status === 401)) startApp();
    },
  );
}

boot();
