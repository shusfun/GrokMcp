import { useEffect, useState, useRef } from "react";
import { Link, useNavigate } from "react-router";
import { getClient } from "../api";
import type { BoundaryEvent } from "../api/client";
import { DurationText } from "./DurationText";
import { ErrorState } from "./EmptyState";
import { JobActions } from "./JobActions";
import { stageViewLabel, waitReasonLabel, type Job } from "../lib/jobs";

export function SessionDetail({ jobId }: { jobId: string }) {
  const client = getClient();
  const navigate = useNavigate();
  const [job, setJob] = useState<Job | null>(null);
  const [events, setEvents] = useState<BoundaryEvent[]>([]);
  const answerEpoch = useRef(0);
  const [readingAnswer, setReadingAnswer] = useState(false);
  const [answer, setAnswer] = useState("");
  const [answerPage, setAnswerPage] = useState<Job["result"]>();
  const [resultError, setResultError] = useState("");
  const [error, setError] = useState("");

  useEffect(() => { answerEpoch.current++; setReadingAnswer(false); setAnswer(""); setAnswerPage(undefined); setResultError(""); }, [jobId, job?.result_turn_id]);

  useEffect(() => {
    const load = () => {
      Promise.all([client.status(jobId), client.events(jobId)])
        .then(([j, ev]) => {
          setJob(j);
          setEvents(ev ?? []);
          setError("");
        })
        .catch((e: Error) => setError(e.message));
    };
    load();
    const off = client.subscribe(load);
    const timer = window.setInterval(load, 30000);
    return () => { off(); window.clearInterval(timer); };
  }, [client, jobId]);

  if (error) return <ErrorState message={error} />;
  if (!job) return <div className="p-4 text-sm text-[var(--muted)]">加载中…</div>;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-auto p-4">
      <Link to={job.project_id ? `/projects/${job.project_id}` : "/"} className="mb-3 text-sm text-[var(--muted)] hover:text-[var(--accent)]">← {job.project_id ? "项目" : "总览"}</Link>
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 className="text-base font-semibold">{job.title}</h1>
        <span className="text-sm text-[var(--muted)]">{stageViewLabel(job.state, job.view_mode)}</span>
        <DurationText seconds={job.elapsed_seconds} />
      </div>
      <p className="mt-1 text-xs text-[var(--muted)]">{job.project} · {job.cwd}</p>
      {job.state === "disconnected" ? <p className="mt-3 text-sm text-[var(--muted)]">任务未自动恢复。点击“继续”将加载原会话。</p> : null}
      {job.view_mode === "headless" && job.input_owner === "tui" ? <p className="mt-3 text-sm text-[var(--muted)]">交互会话仍在后台运行。重新打开终端可继续操作；退出 Grok TUI 后将执行排队请求。</p> : null}
      <p className="mt-3 text-sm">等待原因：{waitReasonLabel(job.wait_reason)} · 队列：{job.queue_length ?? 0}</p>
      <p className="mt-1 text-xs text-[var(--muted)]">请求：{job.request_id ?? "历史任务"} · Turn：{job.active_turn_id ?? "无"}</p>
      <p className="mt-1 text-xs text-[var(--muted)]">最近活动：{job.last_activity_at && !job.last_activity_at.startsWith("0001-") ? new Date(job.last_activity_at).toLocaleString() : "暂未收到活动"} {job.activity_kind ?? ""}</p>
      <p className="mt-3 text-sm">最近动作：{job.last_action ?? "—"}</p>
      {job.last_summary ? <p className="mt-1 text-sm text-[var(--muted)]">{job.last_summary}</p> : null}
      <div className="mt-4">
        <JobActions
          job={job}
          onShowTui={() => client.setView(job.job_id, "headed").then(setJob)}
          onHeadless={() => client.setView(job.job_id, "headless").then(setJob)}
          onCancel={() => client.cancelTurn(job.job_id, job.active_turn_id).then(setJob)}
          onContinue={() => client.continueJob(job.job_id).then(setJob)}
          onOpenDir={() => client.openProject(job.job_id)}
          onPlanDecide={(decide, notes) => client.planDecide(job.job_id, decide, notes, job).then(setJob)}
          onDebug={() => client.debugSet(job.job_id, !job.debug_enabled).then(setJob)}
          onTrace={() => navigate(`/diagnostics?job=${encodeURIComponent(job.job_id)}`)}
          onExport={() => client.debugExport(job.job_id)}
          onCopyState={() => void navigator.clipboard.writeText(JSON.stringify(job, null, 2))}
          onArchive={() => client.archiveJob(job.job_id).then(setJob)}
          onUnarchive={() => client.unarchiveJob(job.job_id).then(setJob)}
          onDelete={() => client.deleteJob(job.job_id).then(() => navigate(job.project_id ? `/projects/${job.project_id}` : "/"))}
        />
      </div>
      {job.result_turn_id && job.request_id ? <section className="mt-4">
        <button className="text-sm text-[var(--accent)]" onClick={() => {
          const epoch = answerEpoch.current; setReadingAnswer(true);
          client.readResult(job.job_id, job.request_id!, answerPage?.turn_id ?? job.result_turn_id, answerPage?.next_offset ?? 0)
            .then(j => { if (epoch === answerEpoch.current && j.result) { setAnswer(v => v + j.result!.text); setAnswerPage(j.result); setResultError(""); } })
            .catch((e: Error) => { if (epoch === answerEpoch.current) setResultError(e.message); })
            .finally(() => { if (epoch === answerEpoch.current) setReadingAnswer(false); });
        }} disabled={readingAnswer || (!!answerPage && !answerPage.has_more)}>{answerPage ? (answerPage.has_more ? "读取下一页" : "已读取完整回答") : "读取本轮最终回答"}</button>
        {resultError ? <p role="alert">{resultError}</p> : null}
        {answer ? <pre className="mt-2 whitespace-pre-wrap text-sm">{answer}</pre> : null}
      </section> : null}
      {job.plan_summary ? (
        <pre className="mt-4 max-h-48 overflow-auto whitespace-pre-wrap rounded-md border border-[var(--line)] bg-[var(--row)] p-3 text-xs">{job.plan_summary}</pre>
      ) : null}
      <section className="mt-5">
        <h2 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--muted)]">边界事件</h2>
        <ul>
          {(events ?? []).map((ev) => (
            <li key={ev.id} className="flex gap-3 border-l-2 border-[var(--line)] py-2 pl-3 text-sm">
              <span className="w-28 shrink-0 text-xs text-[var(--muted)]">{ev.event_type}</span>
              <span>{ev.summary}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
