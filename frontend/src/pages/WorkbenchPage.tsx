import { useEffect, useMemo, useState } from "react";
import { Outlet, useMatch, useNavigate } from "react-router";
import { getClient } from "../api";
import { JobFilters } from "../components/JobFilters";
import { JobList } from "../components/JobList";
import { SessionDetail } from "../components/SessionDetail";
import { EmptyState, ErrorState } from "../components/EmptyState";
import { filterJobs, sortJobs, type Job, type JobFilters as Filters } from "../lib/jobs";
import { cn } from "../lib/cn";

export function WorkbenchPage() {
  const jobId = useMatch("/sessions/:jobId")?.params.jobId;
  const navigate = useNavigate();
  const client = getClient();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState("");
  const [filters, setFilters] = useState<Filters>({ query: "", state: "all", project: "all", view: "all" });

  useEffect(() => {
    const load = () => {
      client.listJobs()
        .then((list) => {
          setJobs(list);
          setError("");
        })
        .catch((e: Error) => setError(e.message));
    };
    load();
    return client.subscribe(load);
  }, [client]);

  const visible = useMemo(() => sortJobs(filterJobs(jobs, filters)), [jobs, filters]);
  const selected = Boolean(jobId);

  return (
    <div className="flex h-full min-h-0">
      <section
        className={cn(
          "flex min-h-0 min-w-0 flex-col border-[var(--line)] bg-[var(--row)]",
          selected ? "w-[min(42%,420px)] shrink-0 border-r max-[899px]:hidden" : "flex-1",
        )}
      >
        <JobFilters jobs={jobs} value={filters} onChange={setFilters} />
        <div className="min-h-0 flex-1 overflow-auto">
          {error ? <ErrorState message={error} /> : null}
          {!error && jobs.length === 0 ? <EmptyState title="没有任务" detail="从 Codex 调用 grok_dispatch 创建。" /> : null}
          {!error && jobs.length > 0 && visible.length === 0 ? <EmptyState title="没有匹配的任务" detail="调整过滤条件。" /> : null}
          {visible.length > 0 ? (
            <JobList
              jobs={visible}
              selectedId={jobId}
              onSelect={(id) => navigate(`/sessions/${id}`)}
              onShowTui={(job) => void client.setView(job.job_id, "headed")}
            />
          ) : null}
        </div>
      </section>
      <section className={cn("min-h-0 min-w-0 flex-1 bg-[var(--surface)]", !selected && "max-[899px]:hidden")}>
        {jobId ? (
          <SessionDetail jobId={jobId} />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-[var(--muted)]">选择一个任务</div>
        )}
      </section>
      <Outlet />
    </div>
  );
}
