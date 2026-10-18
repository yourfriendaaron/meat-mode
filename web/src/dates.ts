// Local-time date helpers. Everything here is pure and takes `now` explicitly.

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

export function addDays(d: Date, n: number): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n, d.getHours(), d.getMinutes(), d.getSeconds(), d.getMilliseconds());
}

export function at(d: Date, hours: number, minutes = 0): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate(), hours, minutes);
}

export function sameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

/** Start of next Monday, strictly after today's date. */
export function nextMonday(now: Date): Date {
  const day = now.getDay(); // 0 Sun .. 6 Sat
  const ahead = ((8 - day) % 7) || 7;
  return startOfDay(addDays(now, ahead));
}

/** "9:00", "17:30". */
export function fmtTime(d: Date): string {
  return `${d.getHours()}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** "Fri, Oct 23". */
export function fmtDay(d: Date): string {
  return `${DAYS[d.getDay()]}, ${MONTHS[d.getMonth()]} ${d.getDate()}`;
}

/** A short label for a moment relative to now: "14:30", "Tomorrow 9:00", "Fri, Oct 23". */
export function fmtWhen(d: Date, now: Date): string {
  if (sameDay(d, now)) return fmtTime(d);
  if (sameDay(d, addDays(now, 1))) return `Tomorrow ${fmtTime(d)}`;
  if (sameDay(d, addDays(now, -1))) return `Yesterday ${fmtTime(d)}`;
  return fmtDay(d);
}

/** fmtWhen with the time on every day: "14:30", "Tomorrow 9:00", "Fri, Oct 23 9:00". */
export function fmtWhenAt(d: Date, now: Date): string {
  const when = fmtWhen(d, now);
  return when.includes(":") ? when : `${when} ${fmtTime(d)}`;
}

/** "35m overdue", "3h overdue", "2d overdue". */
export function fmtOverdue(due: Date, now: Date): string {
  const mins = Math.max(1, Math.floor((now.getTime() - due.getTime()) / 60000));
  if (mins < 60) return `${mins}m overdue`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h overdue`;
  return `${Math.floor(hours / 24)}d overdue`;
}

/** "just now", "5m ago", "3h ago", "2d ago". */
export function fmtAgo(d: Date, now: Date): string {
  const mins = Math.floor((now.getTime() - d.getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

/** Value for an <input type="datetime-local">. */
export function toLocalInput(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}
