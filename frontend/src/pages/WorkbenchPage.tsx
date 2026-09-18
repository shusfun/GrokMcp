import { useCallback, useEffect, useRef, useState } from "react";
import { Outlet, useMatch, useNavigate } from "react-router";
import { getClient } from "../api";
import { JobFilters } from "../components/JobFilters";
import { JobList } from "../components/JobList";
import { SessionDetail } from "../components/SessionDetail";
import { EmptyState, ErrorState } from "../components/EmptyState";
import { mergeJobs, sortJobs, type Job, type JobFilters as Filters } from "../lib/jobs";
import { cn } from "../lib/cn";

const PAGE_SIZE = 40;

export function WorkbenchPage({ projectId, listOnly }: { projectId?: string; listOnly?: boolean }) {
  const jobId = useMatch("/sessions/:jobId")?.params.jobId;
  const navigate = useNavigate();
  const client = getClient();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [filters, setFilters] = useState<Filters>({ query: "", state: "all", project: "all", view: "all", include_archived: false });
  const seq = useRef(0);
  const cursor = useRef("");
  const loading = useRef(false);
  const loadedCount = useRef(0);
  const sentinel = useRef<HTMLDivElement>(null);

  const loadPage = useCallback((reset: boolean) => {
    const id = reset ? ++seq.current : seq.current;
    if (reset) {
      cursor.current = "";
      loading.current = false;
    }
    if (loading.current) return;
    loading.current = true;
    const reqCursor = reset ? "" : cursor.current;
    client.listJobsPage({
      cursor: reqCursor,
      limit: reset ? Math.max(PAGE_SIZE, loadedCount.current || PAGE_SIZE) : PAGE_SIZE,
      include_archived: filters.include_archived,
      query: filters.query,
      state: filters.state,
      project: projectId || filters.project,
      view: filters.view,
    }).then((page) => {
      if (id !== seq.current) return;
      const incoming = page.jobs ?? [];
      setJobs((prev) => {
        const next = sortJobs(reset ? incoming : mergeJobs(prev, incoming));
        loadedCount.current = next.length;
        return next;
      });
      cursor.current = page.next_cursor ?? "";
      setHasMore(Boolean(page.has_more));
      setError("");
    }).catch((e: Error) => {
      if (id !== seq.current) return;
      setError(e.message);
    }).finally(() => {
      if (id === seq.current) loading.current = false;
    });
  }, [client, filters, projectId]);

  useEffect(() => {
    loadPage(true);
  }, [loadPage]);

  useEffect(() => {
    return client.subscribe(() => {
      loadPage(true);
    });
  }, [client, loadPage]);

  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const obs = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && hasMore && !loading.current) {
        loadPage(false);
      }
    });
    obs.observe(el);
    return () => obs.disconnect();
  }, [hasMore, loadPage, jobs.length]);

  const selected = Boolean(jobId) && !listOnly;

  return (
    <div className="flex h-full min-h-0">
      <section
        className={cn(
          "flex min-h-0 min-w-0 flex-col border-[var(--line)] bg-[var(--row)]",
          selected ? "w-[min(42%,420px)] shrink-0 border-r max-[899px]:hidden" : "flex-1",
        )}
      >
        <JobFilters jobs={jobs} hideProject={Boolean(projectId)} value={filters} onChange={setFilters} />
        <div className="min-h-0 flex-1 overflow-auto">
          {error ? <ErrorState message={error} /> : null}
          {!error && jobs.length === 0 ? <EmptyState title="没有任务" detail={filters.include_archived ? "没有归档任务。" : "从 Codex 调用 grok_dispatch 创建。"} /> : null}
          {jobs.length > 0 ? (
            <JobList
              jobs={jobs}
              selectedId={listOnly ? undefined : jobId}
              onSelect={(id) => navigate(`/sessions/${id}`)}
              onShowTui={(job) => void client.setView(job.job_id, "headed")}
            />
          ) : null}
          <div ref={sentinel} className="h-4" />
        </div>
      </section>
      {listOnly ? null : (
        <section className={cn("min-h-0 min-w-0 flex-1 bg-[var(--surface)]", !selected && "max-[899px]:hidden")}>
          {jobId ? (
            <SessionDetail jobId={jobId} />
          ) : (
            <div className="flex h-full items-center justify-center text-sm text-[var(--muted)]">选择一个任务</div>
          )}
        </section>
      )}
      <Outlet />
    </div>
  );
}
