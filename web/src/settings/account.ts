// The Account section: logging out, on a hub with a password. main.ts
// handles the "logout" act, since logging out replaces the whole app.

import { registerSection } from "./registry.ts";

registerSection({
  id: "acct",
  title: "Account",
  order: 30,
  applies: (ctx) => !!ctx.session?.login,
  render: () => '<p class="muted">Logging out ends this device\'s session and stops its reminders.</p><button class="btn" data-act="logout">Log out</button>',
});
