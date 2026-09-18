import { useNavigate } from "react-router";
import type { Job } from "../lib/jobs";
import { stageViewLabel } from "../lib/jobs";
import { DurationText } from "./DurationText";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

const STAGE_TONE: Record<string, string> = {
  executing: "text-[var(--green)]",
  planning: "text-[var(--green)]",
  plan_ready: "text-[var(--wait)]",
  needs_input: "text-[var(--wait)]",
  failed: "text-[var(--fail)]",
  blocked: "text-[var(--fail)]",
  cancelled: "text-[var(--fail)]",
  disconnected: "text-[var(--fail)]",
};

export function JobTable({ jobs }: { jobs: Job[] }) {
  const navigate = useNavigate();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>任务</TableHead>
          <TableHead>项目</TableHead>
          <TableHead>阶段</TableHead>
          <TableHead>最近动作</TableHead>
          <TableHead className="text-right">耗时</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {jobs.map((job) => (
          <TableRow
            key={job.job_id}
            className="cursor-pointer hover:bg-[var(--green-soft)]"
            onClick={() => navigate(`/sessions/${job.job_id}`)}
          >
            <TableCell className="font-medium">{job.title}</TableCell>
            <TableCell className="text-[var(--muted)]">{job.project}</TableCell>
            <TableCell className={STAGE_TONE[job.state] ?? "text-[var(--muted)]"}>
              {stageViewLabel(job.state, job.view_mode)}
            </TableCell>
            <TableCell className="text-[var(--muted)]">{job.last_action ?? "—"}</TableCell>
            <TableCell className="text-right"><DurationText seconds={job.elapsed_seconds} /></TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
