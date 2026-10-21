// Short sounds for done, add, snooze and undo, synthesized with the Web Audio
// API, so the app ships no audio files. Done climbs a step when it follows the
// last done closely, so a run of check-offs plays a rising scale.

export type Sound = "done" | "add" | "snooze" | "undo" | "clear";

const KEY = "meat-mode.sound";

/** Whether this device plays sounds. On unless turned off here. */
export function soundOn(): boolean {
  try {
    return localStorage.getItem(KEY) !== "off";
  } catch {
    return true;
  }
}

export function setSoundOn(on: boolean): void {
  try {
    localStorage.setItem(KEY, on ? "on" : "off");
  } catch {
    // storage blocked: the choice lasts for this page only
  }
}

/** Semitones above the first done, up a major pentatonic scale so a streak always sounds in tune. */
export const STREAK_STEPS = [0, 2, 4, 7, 9, 12, 14, 16, 19];
/** How close a done must follow the last one to climb a step. */
export const STREAK_MS = 4000;

/** The streak's place after a done at `at`, given the last done's time and place. */
export function nextStreak(last: { at: number; step: number } | null, at: number): number {
  if (!last || at - last.at > STREAK_MS) return 0;
  return Math.min(last.step + 1, STREAK_STEPS.length - 1);
}

let streak: { at: number; step: number } | null = null;

let ctx: AudioContext | null = null;
let out: GainNode | null = null;

function audio(): AudioContext | null {
  if (!ctx) {
    if (typeof AudioContext === "undefined") return null;
    try {
      ctx = new AudioContext();
    } catch {
      return null;
    }
    out = ctx.createGain();
    out.gain.value = 0.7;
    const comp = ctx.createDynamicsCompressor();
    comp.threshold.value = -14;
    out.connect(comp).connect(ctx.destination);
  }
  if (ctx.state === "suspended") void ctx.resume();
  return ctx;
}

const note = (midi: number) => 440 * Math.pow(2, (midi - 69) / 12);

function tone(c: AudioContext, o: { f: number; to?: number; at?: number; dur: number; type?: OscillatorType; gain: number }) {
  const t = c.currentTime + (o.at ?? 0);
  const osc = c.createOscillator();
  const g = c.createGain();
  osc.type = o.type ?? "sine";
  osc.frequency.setValueAtTime(o.f, t);
  if (o.to) osc.frequency.exponentialRampToValueAtTime(o.to, t + o.dur * 0.8);
  g.gain.setValueAtTime(0.0001, t);
  g.gain.exponentialRampToValueAtTime(o.gain, t + 0.004);
  g.gain.exponentialRampToValueAtTime(0.0001, t + o.dur);
  osc.connect(g).connect(out!);
  osc.start(t);
  osc.stop(t + o.dur + 0.05);
}

function sounds(c: AudioContext, s: number): Record<Sound, () => void> {
  return {
    done: () => {
      tone(c, { f: note(81 + s), to: note(88 + s), dur: 0.09, gain: 0.28 });
      tone(c, { f: note(88 + s), at: 0.07, dur: 0.2, gain: 0.22 });
    },
    add: () => tone(c, { f: note(72), to: note(79), dur: 0.12, type: "triangle", gain: 0.3 }),
    snooze: () => {
      tone(c, { f: note(76), dur: 0.14, type: "triangle", gain: 0.25 });
      tone(c, { f: note(71), at: 0.12, dur: 0.22, type: "triangle", gain: 0.25 });
    },
    undo: () => tone(c, { f: note(79), to: note(70), dur: 0.16, type: "triangle", gain: 0.28 }),
    clear: () => {
      [72, 76, 79, 84].forEach((n, i) => tone(c, { f: note(n), at: i * 0.08, dur: 0.35, gain: 0.2 }));
      tone(c, { f: note(96), at: 0.32, dur: 0.6, gain: 0.06 });
    },
  };
}

/** Plays a sound where this device has them on. A done also moves the streak on. */
export function play(sound: Sound): void {
  let step = 0;
  if (sound === "done") {
    const at = performance.now();
    step = nextStreak(streak, at);
    streak = { at, step };
  } else if (sound === "undo") {
    streak = null;
  }
  if (!soundOn()) return;
  const c = audio();
  if (!c) return;
  sounds(c, STREAK_STEPS[step])[sound]();
}

/** The Sound section's switch. A click reaches the section as "sound-switch". */
export function soundSwitchHTML(): string {
  const on = soundOn();
  return `<div class="set-switch"><div><div id="sound-label">Sounds on this device</div><div class="muted small">A soft pop when you check off, add, snooze or undo a task. It climbs when you check off several in a row.</div></div><button class="switch" role="switch" id="sound-switch" aria-checked="${on}" aria-labelledby="sound-label" data-act="sound-switch"><span class="switch-knob" aria-hidden="true"></span></button></div>`;
}
