import { useEffect, useMemo, useState } from "react";
import { getClient } from "../api";
import { JobFilters } from "../components/JobFilters";
import { JobTable } from "../components/JobTable";
import { EmptyState, ErrorState } from "../components/EmptyState";
import { filterJobs, type Job, type JobFilters as Filters } from "../lib/jobs";

export function OverviewPage() {
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

  const visible = useMemo(() => filterJobs(jobs, filters), [jobs, filters]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <JobFilters jobs={jobs} value={filters} onChange={setFilters} />
      <div className="min-h-0 flex-1 overflow-auto">
        {error ? <ErrorState message={error} /> : null}
        {!error && jobs.length === 0 ? <EmptyState title="没有任务" detail="从 Codex 调用 grok_dispatch 创建。" /> : null}
        {!error && jobs.length > 0 && visible.length === 0 ? <EmptyState title="没有匹配的任务" detail="调整过滤条件。" /> : null}
        {visible.length > 0 ? <JobTable jobs={visible} /> : null}
      </div>
    </div>
  );
}
