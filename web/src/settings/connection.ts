import { checkButtonHTML, checksHTML, connectionAct } from "../connection.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "connection",
  title: "Connection",
  order: 45,
  render() {
    return `<p class="muted">Checks the address, the secure connection, live updates, the clocks and the reminder keys.</p>
${checksHTML()}${checkButtonHTML()}`;
  },
  act: (act, _el, ctx) => connectionAct(act, ctx),
});
