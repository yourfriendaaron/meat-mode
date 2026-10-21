// The Sound section: whether this device plays the action sounds.

import { play, setSoundOn, soundOn, soundSwitchHTML } from "../sound.ts";
import { registerSection } from "./registry.ts";

registerSection({
  id: "sound",
  title: "Sound",
  order: 11,
  render: () => soundSwitchHTML(),
  act(act, _el, ctx) {
    if (act !== "sound-switch") return;
    setSoundOn(!soundOn());
    play("done");
    ctx.render();
  },
});
