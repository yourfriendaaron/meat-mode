// A value the app loads from the hub: the value, whether a load is under
// way, why the last one failed, and when to load again. Each holder decides
// what to draw; this decides when to ask.

import { errorText } from "./api.ts";

export interface Remote<T> {
  /**
   * The value last loaded, or null. Starts a load when it is stale (see
   * stale); `done` runs when that load ends, such as to draw the app again.
   */
  get(done?: () => void): T | null;
  /** The value last loaded, or null, without starting a load. */
  peek(): T | null;
  /** Why the last load failed, in errorText's words, or "". A load under way clears it. */
  error(): string;
  loading(): boolean;
  /**
   * Whether get would load now: nothing loaded and no error, or the value or
   * error is older than maxAge. Never while a load is under way.
   */
  stale(): boolean;
  /**
   * Loads now. A load already under way is dropped when it lands, so what
   * this one finds wins, such as after a change. Resolves when it ends and
   * never rejects; `done` runs then. A failed load keeps the last value.
   */
  reload(done?: () => void): Promise<void>;
  /** Sets the value as if it had loaded, such as from the hub's answer to a change. A load under way then lands nowhere. */
  set(value: T): void;
  /** Drops the value and the error. A load under way then lands nowhere. */
  forget(): void;
}

export interface RemoteOptions {
  /** Milliseconds after which a value or an error counts as stale. Default never. */
  maxAge?: number;
}

export function remote<T>(load: () => Promise<T>, opts: RemoteOptions = {}): Remote<T> {
  const maxAge = opts.maxAge ?? Infinity;
  let value: T | null = null;
  let error = "";
  /** When the value or the error came, or 0 for neither. */
  let at = 0;
  let pending: Promise<void> | null = null;
  /** Bumped by each load and by forget, so only the latest load lands. */
  let generation = 0;

  const self: Remote<T> = {
    get(done) {
      if (self.stale()) void self.reload(done);
      return value;
    },
    peek: () => value,
    error: () => error,
    loading: () => pending !== null,
    stale() {
      if (pending) return false;
      if (value === null && !error) return true;
      return Date.now() - at > maxAge;
    },
    reload(done) {
      const gen = ++generation;
      error = "";
      const run = (async () => {
        try {
          const v = await load();
          if (gen !== generation) return;
          value = v;
          error = "";
        } catch (e) {
          if (gen !== generation) return;
          error = errorText(e);
        }
        at = Date.now();
      })();
      const p = run.finally(() => {
        if (gen === generation) pending = null;
      });
      pending = p;
      return done ? p.then(done) : p;
    },
    set(v) {
      generation++;
      pending = null;
      value = v;
      error = "";
      at = Date.now();
    },
    forget() {
      generation++;
      value = null;
      error = "";
      at = 0;
      pending = null;
    },
  };
  return self;
}
