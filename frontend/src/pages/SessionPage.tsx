import { useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import { getClient } from "../api";
import type { BoundaryEvent } from "../api/client";
import { DurationText } from "../components/DurationText";
import { ErrorState } from "../components/EmptyState";
import { JobActions } from "../components/JobActions";
import { stageViewLabel, type Job } from "../lib/jobs";

export function SessionPage() {
  const { jobId } = useParams();
  const client = getClient();
  const [job, setJob] = useState<Job | null>(null);
  const [events, setEvents] = useState<BoundaryEvent[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!jobId) return;
    const load = () => {
      Promise.all([client.status(jobId), client.events(jobId)])
        .then(([j, ev]) => {
          setJob(j);
          setEvents(ev);
          setError("");
        })
        .catch((e: Error) => setError(e.message));
    };
    load();
    return client.subscribe(load);
  }, [client, jobId]);

  if (error) return <ErrorState message={error} />;
  if (!job) return <div className="p-4 text-sm text-[var(--muted)]">加载中…</div>;

  return (
    <div className="space-y-5 p-4">
      <Link to="/" className="text-sm text-[var(--muted)] hover:text-[var(--green)]">← 总览</Link>
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 className="text-lg font-semibold">{job.title}</h1>
        <span className="text-sm text-[var(--muted)]">{stageViewLabel(job.state, job.view_mode)}</span>
        <DurationText seconds={job.elapsed_seconds} />
      </div>
      <p className="text-sm text-[var(--muted)]">{job.project} · {job.cwd}</p>
      <p className="text-sm">最近动作：{job.last_action ?? "—"}</p>
      {job.last_summary ? <p className="text-sm text-[var(--muted)]">{job.last_summary}</p> : null}
      {job.plan_summary ? (
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded-md border border-[var(--line)] bg-[var(--row)] p-3 text-xs">{job.plan_summary}</pre>
      ) : null}
      <p className="font-mono text-xs text-[var(--muted)]">{job.grok_session_id}</p>
      <JobActions
        job={job}
        onShowTui={() => client.setView(job.job_id, "headed").then(setJob)}
        onHeadless={() => client.setView(job.job_id, "headless").then(setJob)}
        onCancel={() => client.cancelTurn(job.job_id).then(setJob)}
        onContinue={() => client.continueJob(job.job_id).then(setJob)}
        onOpenDir={() => client.openProject(job.job_id)}
        onPlanDecide={(decide, notes) => client.planDecide(job.job_id, decide, notes).then(setJob)}
      />
      <section>
        <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted)]">边界事件</h2>
        <ul className="divide-y divide-[var(--line)] border-t border-[var(--line)]">
          {events.map((ev) => (
            <li key={ev.id} className="flex gap-3 py-2 text-sm">
              <span className="w-28 shrink-0 text-[var(--muted)]">{ev.event_type}</span>
              <span>{ev.summary}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
