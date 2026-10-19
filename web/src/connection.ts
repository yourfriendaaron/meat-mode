// The connection check: whether the app is reached the way the hub expects,
// shared by the setup wizard's step and the Settings section.

import type { AppContext } from "./app-context.ts";
import { call } from "./api.ts";
import { esc } from "./html.ts";
import type { SetupStatus } from "./types.ts";

export interface Check {
  id: "address" | "dns" | "secure" | "live" | "clock" | "push" | "ntfy" | "tailscale";
  title: string;
  /** "info" is for what is optional or only for the record. */
  state: "pass" | "fail" | "info";
  /** The state's label, when "Optional" does not fit an info line. */
  label?: string;
  detail: string;
  /** What to do about a failure. */
  fix?: string;
}

/** What the browser and the hub told us. */
export interface Probe {
  status: SetupStatus;
  /** location.origin and location.host of this page. */
  origin: string;
  host: string;
  secure: boolean;
  live: "ok" | "timeout" | "unsupported";
  /** This device's clock, in ms, at the middle of the status request. */
  deviceMs: number;
}

/** A device clock further from the hub's than this is reported. */
export const CLOCK_LIMIT_MS = 60_000;
/** How long live updates have to arrive. */
export const LIVE_WAIT_MS = 5000;

function sameOrigin(a: string, b: string): boolean {
  try {
    return new URL(a).origin === new URL(b).origin;
  } catch {
    return false;
  }
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

/** The fix for a public address, shared by the check and the warning in Settings. */
export const PUBLIC_DNS_FIX =
  "The hub must be reachable only on your private network. If you turned on Tailscale Funnel for it, turn Funnel off; its public records can take a few hours to go away. Otherwise remove the public DNS record for this name, or point it at a private address.";

/** What public DNS says of the hub's name, when the internet can find it; otherwise null. */
export function publicDnsWarning(status: SetupStatus): { detail: string; fix: string } | null {
  if (status.publicDns?.state !== "public") return null;
  const addrs = status.publicDns.addresses ?? [];
  const list = addrs.length ? addrs.join(", ") : "a public address";
  return {
    detail: `Public DNS gives ${hostOf(status.publicUrl)} the address${addrs.length === 1 ? "" : "es"} ${list}, so anyone on the internet can look it up.`,
    fix: PUBLIC_DNS_FIX,
  };
}

function dnsCheck(status: SetupStatus): Check {
  const warn = publicDnsWarning(status);
  if (warn) return { id: "dns", title: "Public DNS", state: "fail", ...warn };
  const host = hostOf(status.publicUrl);
  switch (status.publicDns?.state) {
    case "private":
      return { id: "dns", title: "Public DNS", state: "pass", detail: `Public DNS has no public address for ${host}.` };
    case "skipped":
      return { id: "dns", title: "Public DNS", state: "pass", detail: `${host} is localhost or a private address, so there is nothing to look up.` };
    default:
      return {
        id: "dns",
        title: "Public DNS",
        state: "info",
        label: "Not checked",
        detail: `The hub could not ask public DNS about ${host} yet. It tries again every hour.`,
      };
  }
}

function minutes(ms: number): string {
  const m = Math.max(1, Math.round(Math.abs(ms) / 60_000));
  return m === 1 ? "about a minute" : `about ${m} minutes`;
}

/** Turns what was learned into one result per check. */
export function evaluate(p: Probe): Check[] {
  const { status } = p;
  const out: Check[] = [];

  const addressOK = status.hostMatches && sameOrigin(p.origin, status.publicUrl);
  out.push(
    addressOK
      ? { id: "address", title: "Address", state: "pass", detail: `This page is open at ${p.host}, the address the hub is set up for.` }
      : {
          id: "address",
          title: "Address",
          state: "fail",
          detail: `This page is open at ${p.origin}, but the hub is set up for ${status.publicUrl}.`,
          fix: `Open the app at ${status.publicUrl}. If you use another address on purpose, set MEAT_MODE_PUBLIC_URL to it and restart the hub. Behind a reverse proxy, make sure it passes the original Host header.`,
        },
  );

  out.push(dnsCheck(status));

  out.push(
    p.secure
      ? { id: "secure", title: "Secure connection", state: "pass", detail: "This page is a secure context, so reminders and installing work." }
      : {
          id: "secure",
          title: "Secure connection",
          state: "fail",
          detail: "This page is not a secure context.",
          fix: "Browsers allow reminders and installing to the home screen only on https:// addresses, or on localhost. Open the app at its https:// address.",
        },
  );

  out.push(
    p.live === "ok"
      ? { id: "live", title: "Live updates", state: "pass", detail: "A test update arrived at once." }
      : {
          id: "live",
          title: "Live updates",
          state: "fail",
          detail:
            p.live === "unsupported"
              ? "This browser cannot receive live updates."
              : `No update arrived within ${LIVE_WAIT_MS / 1000} seconds.`,
          fix:
            p.live === "unsupported"
              ? "Use a current browser. Without live updates the app only shows changes when you reload."
              : "A reverse proxy in front of the hub is probably holding the stream back. Turn off response buffering for /api/events (with nginx, proxy_buffering off) and make sure nothing compresses it, then check again.",
        },
  );

  const skew = p.deviceMs - Date.parse(status.time);
  out.push(
    Math.abs(skew) <= CLOCK_LIMIT_MS
      ? { id: "clock", title: "Clocks", state: "pass", detail: "This device and the hub agree on the time." }
      : {
          id: "clock",
          title: "Clocks",
          state: "fail",
          detail: `This device's clock is ${minutes(skew)} ${skew > 0 ? "ahead of" : "behind"} the hub's.`,
          fix: "Turn on automatic date and time on this device. If the hub's clock is the wrong one, fix the clock of the machine it runs on. Reminders and due dates depend on both.",
        },
  );

  out.push(
    status.pushKeys
      ? { id: "push", title: "Reminder keys", state: "pass", detail: "The hub has the keys it needs to send reminders." }
      : {
          id: "push",
          title: "Reminder keys",
          state: "fail",
          detail: "The hub has no Web Push keys.",
          fix: "The hub makes them on first start and keeps them in its data folder. Check the hub's log for an error about that folder, make sure it is writable, then restart the hub.",
        },
  );

  out.push(
    status.ntfy
      ? { id: "ntfy", title: "ntfy", state: "pass", detail: "The hub is set up to send reminders to ntfy as well." }
      : {
          id: "ntfy",
          title: "ntfy",
          state: "info",
          detail: "Not set up. This is optional: it adds a second way to get reminders.",
        },
  );

  out.push({
    id: "tailscale",
    title: "Tailscale identity",
    state: "info",
    label: "Info",
    detail: status.tailscaleIdentity
      ? "This request came through Tailscale Serve, which told the hub which tailnet user sent it."
      : "This request carried no Tailscale identity. That is expected on a LAN or behind another reverse proxy; through Tailscale Serve it would carry one.",
  });
  return out;
}

// ---------- running the check ----------

let result: Check[] | null = null;
let failure = "";
let running = false;
let checkedAt = 0;
/** The latest GET /api/setup/status, from a check or from Settings, and when it came. */
let latest: SetupStatus | null = null;
let latestAt = 0;
let loading = false;

/** Whether the last check ran and found no failure. */
export function connectionOK(): boolean {
  return result !== null && !result.some((c) => c.state === "fail");
}

function revOf(e: Event): number {
  try {
    return Number(JSON.parse((e as MessageEvent<string>).data).rev);
  } catch {
    return NaN;
  }
}

/**
 * Opens an event stream of its own, asks the hub for a test event once the
 * stream has answered, and waits for it. A proxy that buffers the stream
 * holds the first event back, or this one.
 */
function probeLive(): Promise<Probe["live"]> {
  if (typeof EventSource === "undefined") return Promise.resolve("unsupported");
  return new Promise((resolve) => {
    const es = new EventSource("/api/events");
    let first: number | null = null;
    const finish = (v: Probe["live"]) => {
      clearTimeout(timer);
      es.close();
      resolve(v);
    };
    const timer = setTimeout(() => finish("timeout"), LIVE_WAIT_MS);
    es.addEventListener("change", (e) => {
      const rev = revOf(e);
      if (first === null) {
        // The hub sends its revision when a stream opens. The test event raises it.
        first = rev;
        call<void>("POST", "/api/setup/test-event").catch(() => finish("timeout"));
      } else if (rev > first) {
        finish("ok");
      }
    });
  });
}

/** Runs every check, drawing the app before and after. */
export async function runChecks(ctx: AppContext): Promise<void> {
  if (running) return;
  running = true;
  result = null;
  failure = "";
  ctx.render();
  try {
    const sent = Date.now();
    const liveProbe = probeLive();
    const status = await call<SetupStatus>("GET", "/api/setup/status");
    latest = status;
    latestAt = Date.now();
    const received = Date.now();
    const live = await liveProbe;
    // The hub stamped its time somewhere in the status request, so use the middle of it.
    const deviceMs = (sent + received) / 2;
    result = evaluate({
      status,
      origin: location.origin,
      host: location.host,
      secure: window.isSecureContext,
      live,
      deviceMs,
    });
  } catch (e) {
    failure = `Could not reach the hub: ${(e as Error).message}`;
  } finally {
    // A failed run counts as recent too, so the error stays up until the user asks again.
    checkedAt = Date.now();
    running = false;
    ctx.render();
  }
}

/** Starts a check when none has run lately; for a step that opens on one. */
export function checkIfStale(ctx: AppContext): void {
  if (typeof document === "undefined" || running) return;
  if (result === null && !failure) void runChecks(ctx);
  else if (Date.now() - checkedAt > 60_000) void runChecks(ctx);
}

const STATE = { pass: "Passed", fail: "Problem", info: "Optional" } as const;

/** The results, or what is going on in their place. */
export function checksHTML(): string {
  if (running) return '<p class="muted" role="status">Checking&hellip;</p>';
  if (failure) return `<p class="conn-error" role="alert">${esc(failure)}</p>`;
  if (!result) return '<p class="muted">Not checked yet.</p>';
  return `<ul class="conn-list" aria-label="Connection checks">${result
    .map(
      (c) => `<li class="conn conn-${c.state}"><span class="conn-state">${esc(c.label ?? STATE[c.state])}</span><div><strong>${esc(c.title)}</strong>
<p>${esc(c.detail)}</p>${c.fix ? `<p class="conn-fix">${esc(c.fix)}</p>` : ""}</div></li>`,
    )
    .join("")}</ul>`;
}

export function checkButtonHTML(): string {
  return `<div class="btn-row"><button class="btn" data-act="connection-run"${running ? " disabled" : ""}>${result || failure ? "Check again" : "Check connection"}</button></div>`;
}

/** The click handler both the step and the Settings section use. */
export function connectionAct(act: string, ctx: AppContext): void {
  if (act === "connection-run") void runChecks(ctx);
}

// ---------- the public DNS warning in Settings ----------

/**
 * Asks the hub for its status when Settings has not lately, without running
 * the whole check, so the public DNS warning shows when Settings opens.
 */
export function loadStatusIfStale(ctx: AppContext): void {
  if (typeof document === "undefined" || loading || running) return;
  if (latest && Date.now() - latestAt < 60_000) return;
  loading = true;
  call<SetupStatus>("GET", "/api/setup/status").then(
    (s) => {
      latest = s;
      latestAt = Date.now();
      loading = false;
      ctx.render();
    },
    () => {
      // The check shows the error when the user runs it.
      latestAt = Date.now();
      loading = false;
    },
  );
}

/** The warning Settings shows while public DNS gives the hub's name a public address, or "". */
export function publicDnsWarningHTML(): string {
  const w = latest && publicDnsWarning(latest);
  if (!w) return "";
  return `<div class="conn-warn" role="alert"><strong>The internet can find this hub</strong><p>${esc(w.detail)}</p><p class="conn-fix">${esc(w.fix)}</p></div>`;
}
