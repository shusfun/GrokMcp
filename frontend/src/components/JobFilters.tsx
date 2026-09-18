import type { ReactNode } from "react";
import type { Job, JobFilters as Filters } from "../lib/jobs";
import { summarizeStatus } from "../lib/jobs";
import { cn } from "../lib/cn";
import { Command } from "./ui/command";
import { Select } from "./ui/select";

const STATES = [
  { value: "all", label: "状态：全部" },
  { value: "attention", label: "需输入" },
  { value: "working", label: "工作中" },
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

function Chip({
  active,
  children,
  onClick,
}: {
  active: boolean;
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "h-7 rounded-md px-2 text-xs font-medium",
        active ? "bg-[var(--accent-soft)] text-[var(--accent)]" : "text-[var(--muted)] hover:text-[var(--ink)]",
      )}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

export function JobFilters({
  jobs,
  projects = [],
  hideProject = false,
  value,
  onChange,
}: {
  jobs: Job[];
  projects?: string[];
  hideProject?: boolean;
  value: Filters;
  onChange: (v: Filters) => void;
}) {
  const sum = summarizeStatus(jobs);
  const toggle = (state: string) => onChange({ ...value, state: value.state === state ? "all" : state });
  return (
    <div className="space-y-2 border-b border-[var(--line)] bg-[var(--row)] px-3 py-2">
      <div className="flex items-center gap-1">
        <Command value={value.query} onChange={(query) => onChange({ ...value, query })} placeholder="搜索任务" />
        <Chip active={value.state === "attention"} onClick={() => toggle("attention")}>需输入 {sum.needsInput}</Chip>
        <Chip active={value.state === "working"} onClick={() => toggle("working")}>工作 {sum.working}</Chip>
        <Chip active={value.include_archived} onClick={() => onChange({ ...value, include_archived: !value.include_archived })}>归档</Chip>
      </div>
      <div className="flex gap-2">
        <Select aria-label="状态" className="min-w-0 flex-1" value={value.state} options={STATES} onChange={(state) => onChange({ ...value, state })} />
        {hideProject ? null : (
          <Select
            aria-label="项目"
            className="min-w-0 flex-1"
            value={value.project}
            options={[{ value: "all", label: "项目：全部" }, ...projects.map((p) => ({ value: p, label: p }))]}
            onChange={(project) => onChange({ ...value, project })}
          />
        )}
        <Select aria-label="形态" className="min-w-0 flex-1" value={value.view} options={VIEWS} onChange={(view) => onChange({ ...value, view })} />
      </div>
    </div>
  );
}
