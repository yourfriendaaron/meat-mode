// The calendar feed (docs/api.md, Calendar feed): a private link calendar
// apps subscribe to, shown once when made.

import type { AppContext } from "../app-context.ts";
import { call, errorText } from "../api.ts";
import { copyText } from "../clipboard.ts";
import { esc } from "../html.ts";
import { remote } from "../remote.ts";
import type { CalendarLink, CalendarStatus } from "../types.ts";
import { registerSection } from "./registry.ts";

/** Whether the feed is on. */
const feedOn = remote(async () => (await call<CalendarStatus>("GET", "/api/calendar")).on);

const cal = {
  /** The link just made, shown until Done or leaving Settings. */
  link: "",
  copied: false,
  /** Asking before a new link replaces the old one. */
  confirmNew: false,
  busy: false,
};

/** Forgets the link on screen; it is shown once. */
function forgetCalendarLink(): void {
  cal.link = "";
  cal.copied = false;
  cal.confirmNew = false;
}

/** Runs a change to the feed, then draws the section and puts focus on `focus`. */
async function change(ctx: AppContext, fn: () => Promise<void>, focus: string) {
  if (cal.busy) return;
  cal.busy = true;
  try {
    await fn();
  } catch (e) {
    ctx.toast(`Could not change the calendar link: ${errorText(e)}`);
  }
  cal.busy = false;
  ctx.render();
  document.getElementById(focus)?.focus();
}

function newLink(ctx: AppContext) {
  return change(
    ctx,
    async () => {
      cal.confirmNew = false;
      cal.link = (await call<CalendarLink>("POST", "/api/calendar", {})).url;
      cal.copied = false;
      feedOn.set(true);
    },
    "cal-copy",
  );
}

async function copy(ctx: AppContext) {
  if (!document.getElementById("cal-url")) return;
  await copyText("cal-url");
  cal.copied = true;
  ctx.render();
}

const REACH =
  '<p class="muted small">Anyone with the link can see the titles and due times, so keep it to yourself. The hub answers only on your private network, so the link works in a calendar app on your own phone or computer while it is on that network. Google Calendar and Outlook on the web fetch links from their own servers, which cannot reach the hub.</p>';

registerSection({
  id: "calendar",
  title: "Calendar",
  order: 25,
  leave: forgetCalendarLink,
  render(ctx) {
    const on = feedOn.get(ctx.render);
    if (on === null) {
      if (feedOn.error()) {
        return `<p class="row-error">Could not load the calendar link: ${esc(feedOn.error())}</p><button class="btn" data-act="calendar-retry">Try again</button>`;
      }
      return '<p class="muted">Loading</p>';
    }
    const busy = cal.busy ? " disabled" : "";
    if (cal.link) {
      return `<div class="token-box" role="status"><p>In your calendar app, add a calendar from a link (often called Subscribe or From URL) and paste this one. It is shown only once.</p><code class="cmd" id="cal-url">${esc(cal.link)}</code><div class="btn-row"><button class="btn" id="cal-copy" data-act="calendar-copy">${cal.copied ? "Copied" : "Copy"}</button><button class="btn" data-act="calendar-done">Done</button></div><p class="muted small">Calendar apps check the link on their own schedule, from every hour to once a day.</p></div>${REACH}`;
    }
    if (!on) {
      return `<p>Show every open to-do with a due date, from every list, in a calendar app.</p><button class="btn btn-primary" id="cal-on" data-act="calendar-on"${busy}>Get calendar link</button>${REACH}`;
    }
    const buttons = cal.confirmNew
      ? `<p class="confirm" id="cal-confirm">The current link stops working, and calendar apps using it need the new one.</p><div class="btn-row"><button class="btn btn-danger" data-act="calendar-new-confirm"${busy}>Make a new link</button><button class="btn" id="cal-cancel" data-act="calendar-new-cancel">Cancel</button></div>`
      : `<div class="btn-row"><button class="btn" id="cal-new" data-act="calendar-new"${busy}>New link</button><button class="btn" data-act="calendar-off"${busy}>Turn off</button></div>`;
    return `<p>The calendar link is on. Make a new link if the old one got out, or turn it off to stop it working.</p>${buttons}${REACH}`;
  },
  async act(act, _el, ctx) {
    switch (act) {
      case "calendar-retry":
        await feedOn.reload(ctx.render);
        break;
      case "calendar-on":
      case "calendar-new-confirm":
        await newLink(ctx);
        break;
      case "calendar-new":
        cal.confirmNew = true;
        ctx.render();
        document.getElementById("cal-cancel")?.focus();
        break;
      case "calendar-new-cancel":
        cal.confirmNew = false;
        ctx.render();
        document.getElementById("cal-new")?.focus();
        break;
      case "calendar-off":
        await change(
          ctx,
          async () => {
            await call<void>("DELETE", "/api/calendar");
            feedOn.set(false);
          },
          "cal-on",
        );
        break;
      case "calendar-copy":
        await copy(ctx);
        break;
      case "calendar-done":
        forgetCalendarLink();
        ctx.render();
        document.getElementById("cal-new")?.focus();
        break;
    }
  },
});
