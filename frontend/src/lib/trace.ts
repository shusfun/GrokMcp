import type { TraceEvent } from "../api/client";

export function filterTrace(events: TraceEvent[], level: string, source: string): TraceEvent[] {
  return events.filter((ev) => {
    if (level && level !== "all" && ev.level !== level) return false;
    if (source && source !== "all" && ev.source !== source) return false;
    return true;
  });
}

export function formatTraceLine(ev: TraceEvent): string {
  const time = ev.time.slice(11, 19) || ev.time;
  const bits = [time, ev.event];
  if (ev.fields?.reason) bits.push(`reason=${String(ev.fields.reason)}`);
  if (ev.message && ev.message !== ev.event) bits.push(ev.message);
  return bits.join("  ");
}
