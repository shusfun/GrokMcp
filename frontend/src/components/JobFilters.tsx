import type { Job, JobFilters as Filters } from "../lib/jobs";
import { projectOptions } from "../lib/jobs";
import { Command } from "./ui/command";
import { Select } from "./ui/select";

const STATES = [
  { value: "all", label: "状态：全部" },
  { value: "executing", label: "实施中" },
  { value: "plan_ready", label: "Plan 就绪" },
  { value: "needs_input", label: "需要输入" },
  { value: "planning", label: "规划中" },
  { value: "completed", label: "已完成" },
  { value: "failed", label: "失败" },
];

const VIEWS = [
  { value: "all", label: "形态：全部" },
  { value: "headless", label: "无头" },
  { value: "headed", label: "有头" },
];

export function JobFilters({ jobs, value, onChange }: { jobs: Job[]; value: Filters; onChange: (v: Filters) => void }) {
  const projects = projectOptions(jobs);
  return (
    <div className="grid grid-cols-1 gap-2 border-b border-[var(--line)] bg-[var(--row)] p-2.5 sm:grid-cols-[minmax(0,1fr)_140px_140px_140px]">
      <Command value={value.query} onChange={(query) => onChange({ ...value, query })} placeholder="搜索任务" />
      <Select aria-label="状态" value={value.state} options={STATES} onChange={(state) => onChange({ ...value, state })} />
      <Select
        aria-label="项目"
        value={value.project}
        options={[{ value: "all", label: "项目：全部" }, ...projects.map((p) => ({ value: p, label: p }))]}
        onChange={(project) => onChange({ ...value, project })}
      />
      <Select aria-label="形态" value={value.view} options={VIEWS} onChange={(view) => onChange({ ...value, view })} />
    </div>
  );
}
