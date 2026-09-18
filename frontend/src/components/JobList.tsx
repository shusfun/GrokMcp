import { SquareTerminal } from "lucide-react";
import type { Job } from "../lib/jobs";
import { stageLabel, viewLabel } from "../lib/jobs";
import { cn } from "../lib/cn";
import { DurationText } from "./DurationText";
import { Button } from "./ui/button";

const DOT: Record<string, string> = {
  executing: "bg-[var(--green)]",
  planning: "bg-[var(--green)]",
  starting: "bg-[var(--green)]",
  recovering: "bg-[var(--green)]",
  plan_ready: "bg-[var(--wait)]",
  needs_input: "bg-[var(--wait)]",
  failed: "bg-[var(--fail)]",
  blocked: "bg-[var(--fail)]",
  cancelled: "bg-[var(--fail)]",
  disconnected: "bg-[var(--fail)]",
};

export function JobList({
  jobs,
  selectedId,
  onSelect,
  onShowTui,
}: {
  jobs: Job[];
  selectedId?: string;
  onSelect: (jobId: string) => void;
  onShowTui: (job: Job) => void;
}) {
  return (
    <ul className="divide-y divide-[var(--line)]">
      {jobs.map((job) => {
        const selected = job.job_id === selectedId;
        return (
          <li key={job.job_id}>
            <div
              className={cn(
                "group flex cursor-pointer items-stretch border-l-2",
                selected ? "border-[var(--accent)] bg-[var(--accent-soft)]" : "border-transparent hover:bg-[var(--surface)]",
              )}
            >
              <button
                type="button"
                className="flex min-w-0 flex-1 items-center gap-3 px-3 py-2 text-left"
                onClick={() => onSelect(job.job_id)}
              >
                <span className={cn("h-2 w-2 shrink-0 rounded-full", DOT[job.state] ?? "bg-[var(--muted)]")} />
                <span className="min-w-0 flex-1">
                  <span className="flex items-baseline justify-between gap-2">
                    <span className="truncate text-sm font-medium">{job.title}</span>
                    <DurationText seconds={job.elapsed_seconds} />
                  </span>
                  <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-[var(--muted)]">
                    <span className="truncate">{job.last_action ?? stageLabel(job.state)}</span>
                    <span>·</span>
                    <span className="shrink-0">{job.project}</span>
                    <span>·</span>
                    <span className="shrink-0">{viewLabel(job.view_mode)}</span>
                  </span>
                </span>
              </button>
              {job.view_mode !== "headed" ? (
                <Button
                  size="icon"
                  variant="ghost"
                  className="my-auto mr-1 hidden group-hover:inline-flex"
                  title="显示 TUI"
                  aria-label="显示 TUI"
                  onClick={(e) => {
                    e.stopPropagation();
                    onShowTui(job);
                  }}
                >
                  <SquareTerminal className="h-3.5 w-3.5" />
                </Button>
              ) : null}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
