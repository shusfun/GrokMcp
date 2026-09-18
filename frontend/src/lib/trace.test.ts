import { describe, expect, it } from "vitest";
import { filterTrace, formatTraceLine } from "./trace";
import type { TraceEvent } from "../api/client";

function ev(partial: Partial<TraceEvent>): TraceEvent {
  return {
    seq: 1,
    time: "2026-09-18T17:10:01.000+08:00",
    level: "info",
    source: "fifo",
    event: "queue.enqueued",
    ...partial,
  };
}

describe("trace helpers", () => {
  it("filters by level and source", () => {
    const events = [
      ev({ seq: 1, event: "queue.enqueued" }),
      ev({ seq: 2, level: "warn", source: "supervisor", event: "watchdog.stalled" }),
    ];
    expect(filterTrace(events, "warn", "all")).toHaveLength(1);
    expect(filterTrace(events, "all", "fifo")).toHaveLength(1);
  });

  it("formats a compact line", () => {
    expect(formatTraceLine(ev({ fields: { reason: "tui_owns" }, event: "pump.skipped" }))).toContain("reason=tui_owns");
  });
});
