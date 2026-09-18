import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { getClient } from "../api";
import type { DebugSnapshot, StatusBar as StatusBarData, TraceEvent } from "../api/client";
import { Button } from "../components/ui/button";
import { Select } from "../components/ui/select";
import { filterTrace, formatTraceLine } from "../lib/trace";
import type { Job } from "../lib/jobs";

function Dot({ ok, warn }: { ok: boolean; warn?: boolean }) {
  const color = warn ? "bg-[var(--fail)]" : ok ? "bg-[var(--green)]" : "bg-[var(--fail)]";
  return <span className={`inline-block h-1.5 w-1.5 rounded-full ${color}`} />;
}

export function DiagnosticsPage() {
  const client = getClient();
  const [params, setParams] = useSearchParams();
  const selected = params.get("job") ?? "";
  const [jobs, setJobs] = useState<Job[]>([]);
  const [bar, setBar] = useState<StatusBarData | null>(null);
  const [events, setEvents] = useState<TraceEvent[]>([]);
  const [cursor, setCursor] = useState(0);
  const [level, setLevel] = useState("all");
  const [source, setSource] = useState("all");
  const [paused, setPaused] = useState(false);
  const [note, setNote] = useState("");

  useEffect(() => {
    const load = () => {
      void client.listJobs().then(setJobs);
      void client.statusBar().then(setBar);
    };
    load();
    return client.subscribe(load);
  }, [client]);

  useEffect(() => {
    if (paused || !selected) return;
    let cancelled = false;
    const tick = () => {
      void client.debugSnapshot(selected, cursor, 200).then((snap: DebugSnapshot) => {
        if (cancelled || snap.events.length === 0) return;
        setEvents((prev) => [...prev, ...snap.events]);
        setCursor(snap.cursor);
      });
    };
    tick();
    const id = window.setInterval(tick, 500);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [client, selected, cursor, paused]);

  const visible = useMemo(() => filterTrace(events, level, source), [events, level, source]);
  const job = jobs.find((j) => j.job_id === selected);

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--surface)]">
      <div className="flex flex-wrap items-center gap-3 border-b border-[var(--line)] px-4 py-2 text-[11px] text-[var(--muted)]">
        <span className="flex items-center gap-1.5"><Dot ok={Boolean(bar?.leader_ok)} /> Leader</span>
        <span className="flex items-center gap-1.5"><Dot ok={Boolean(bar?.acp_ok ?? bar?.leader_ok)} /> ACP</span>
        <span className="flex items-center gap-1.5"><Dot ok={Boolean(bar?.mcp_ok)} /> MCP</span>
        <span className="flex items-center gap-1.5"><Dot ok={bar?.db_ok !== false} /> DB</span>
        <span className="flex items-center gap-1.5">
          <Dot ok={!bar?.stalled} warn={bar?.stalled} />
          {bar?.debug_enabled ? "Debug ON" : "Debug OFF"}
          {bar?.stalled ? " · stalled" : ""}
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-2 border-b border-[var(--line)] px-4 py-2">
        <Select
          aria-label="任务"
          value={selected}
          onChange={(id) => {
            setParams(id ? { job: id } : {});
            setEvents([]);
            setCursor(0);
          }}
          options={[{ value: "", label: "选择任务" }, ...jobs.map((j) => ({ value: j.job_id, label: j.title || j.job_id }))]}
        />
        <Select aria-label="Level" value={level} onChange={setLevel} options={["all", "debug", "info", "warn", "error"].map((v) => ({ value: v, label: v }))} />
        <Select aria-label="Source" value={source} onChange={setSource} options={["all", "supervisor", "fifo", "acp", "terminal", "mcp", "leader", "store"].map((v) => ({ value: v, label: v }))} />
        <Button size="sm" variant="outline" onClick={() => setPaused((v) => !v)}>{paused ? "继续滚动" : "暂停滚动"}</Button>
        <Button
          size="sm"
          variant="outline"
          disabled={!selected}
          onClick={() => {
            void client.debugExport(selected).then((r) => setNote(r.path || "已导出"));
          }}
        >
          导出
        </Button>
        <Button size="sm" variant="ghost" onClick={() => { setEvents([]); setNote(""); }}>清空视图</Button>
        {job?.stalled ? <span className="text-xs text-[var(--fail)]">{job.stalled_reason}</span> : null}
        {note ? <span className="text-xs text-[var(--accent)]">{note}</span> : null}
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-3 font-mono text-xs leading-6">
        {visible.length === 0 ? (
          <p className="text-[var(--muted)]">选择任务后显示结构化 trace。不会渲染 TUI ANSI。</p>
        ) : visible.map((ev) => (
          <div key={`${ev.job_id}-${ev.seq}`} className={ev.level === "error" || ev.level === "warn" ? "text-[var(--fail)]" : ""}>
            {formatTraceLine(ev)}
          </div>
        ))}
      </div>
    </div>
  );
}
