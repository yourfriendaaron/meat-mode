// The Devices section: every place reminders go, with Revoke (docs/api.md,
// Devices).

import { api, errorText } from "../api.ts";
import { fmtAgo } from "../dates.ts";
import { esc } from "../html.ts";
import { remote } from "../remote.ts";
import type { Device } from "../types.ts";
import { registerSection } from "./registry.ts";

/** The hub's list of devices. */
const devices = remote(() => api.devices());

const dev = {
  /** The device being asked about before it is revoked. */
  revoking: null as string | null,
  busy: false,
};

/** Drops the loaded list so the next time Settings draws it, it is fetched again. */
export function forgetDevices(): void {
  devices.forget();
  dev.revoking = null;
}

export function kindLabel(kind: Device["kind"]): string {
  return kind === "app" ? "Android app" : "Browser";
}

/** One line about when the device last received a push and was last seen. */
export function deviceStatus(d: Device, now: Date): string {
  if (!d.confirmed) return "Waiting for confirmation";
  const push = d.lastPush ? `Last push ${fmtAgo(new Date(d.lastPush), now)}` : "No push yet";
  return `${push} · Last seen ${fmtAgo(new Date(d.lastSeen), now)}`;
}

/** What revoking this kind of device does, since the list cannot say which one is this device. */
export function revokeEffect(kind: Device["kind"]): string {
  return kind === "app"
    ? "The app forgets its device token the next time it uses it and shows reminders as off. Turn them on again in the app to get them back."
    : "Reminders stop for that browser until it opens Meat Mode again with notifications allowed, when it signs up anew. To stop it for good, log it out or block its notifications. If it is this browser, that happens the next time you open the app.";
}

registerSection({
  id: "devices",
  title: "Devices",
  order: 22,
  leave: forgetDevices,
  render(ctx) {
    const list = devices.get(ctx.render);
    if (list === null) {
      if (devices.error()) {
        return `<p class="row-error">Could not load the devices: ${esc(devices.error())}</p><button class="btn" data-act="devices-retry">Try again</button>`;
      }
      return '<p class="muted">Loading</p>';
    }
    if (!list.length) return '<p class="muted">No device gets reminders yet. Turn them on above.</p>';
    const n = ctx.now();
    const busy = dev.busy ? " disabled" : "";
    const rows = list
      .map((d) => {
        const asking = dev.revoking === d.id;
        const actions = asking
          ? `<button class="btn btn-small btn-danger" data-act="device-revoke-confirm" data-id="${esc(d.id)}"${busy}>Revoke</button><button class="btn btn-small" id="device-cancel" data-act="device-revoke-cancel">Cancel</button>`
          : `<button class="btn btn-small" data-act="device-revoke" data-id="${esc(d.id)}" aria-label="Revoke ${esc(d.name)}">Revoke</button>`;
        const ask = asking ? `<p class="confirm small" role="status">Revoke ${esc(d.name)}? ${esc(revokeEffect(d.kind))}</p>` : "";
        return `<li class="machine" data-device="${esc(d.id)}"><div><div class="m-name">${esc(d.name)}</div><div class="muted small">${esc(kindLabel(d.kind))} · ${esc(deviceStatus(d, n))}</div>${ask}</div><div class="m-actions">${actions}</div></li>`;
      })
      .join("");
    return `<ul class="machines">${rows}</ul><p class="muted small">Revoke a device to stop reminders to it.</p>`;
  },
  async act(act, el, ctx) {
    switch (act) {
      case "devices-retry":
        await devices.reload(ctx.render);
        break;
      case "device-revoke":
        dev.revoking = el.dataset.id ?? null;
        ctx.render();
        document.getElementById("device-cancel")?.focus();
        break;
      case "device-revoke-cancel":
        dev.revoking = null;
        ctx.render();
        break;
      case "device-revoke-confirm": {
        const id = el.dataset.id ?? "";
        if (dev.busy) return;
        dev.busy = true;
        try {
          await api.revokeDevice(id);
          dev.revoking = null;
          devices.set((devices.peek() ?? []).filter((d) => d.id !== id));
          ctx.toast("Revoked");
        } catch (e) {
          ctx.toast(`Could not revoke the device: ${errorText(e)}`);
        }
        dev.busy = false;
        ctx.render();
        break;
      }
    }
  },
});
