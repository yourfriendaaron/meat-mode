import { checkButtonHTML, checkIfStale, checksHTML, connectionAct, connectionOK } from "../../connection.ts";
import { ORDER, registerStep } from "../registry.ts";

registerStep({
  id: "connection",
  title: "Connection check",
  part: "hub",
  order: ORDER.connection,
  settings: "s-connection",
  done: () => connectionOK(),
  render(ctx) {
    checkIfStale(ctx);
    return `<p>This checks that the app is reached the way the hub expects, and that reminders and live updates can work.</p>
${checksHTML()}${checkButtonHTML()}`;
  },
  act: (act, _el, ctx) => connectionAct(act, ctx),
});
