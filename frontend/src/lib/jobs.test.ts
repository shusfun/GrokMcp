import { describe, expect, it } from "vitest";
import { filterJobs, formatElapsed, sortJobs, stageViewLabel, summarizeStatus, type Job } from "./jobs";

function job(partial: Partial<Job>): Job {
  return {
    job_id: "1",
    cwd: "/tmp/suiyuan",
    project: "suiyuan",
    title: "Runtime 底座",
    state: "executing",
    view_mode: "headless",
    input_owner: "supervisor",
    last_action: "Running tests",
    elapsed_seconds: 18 * 60,
    created_at: "",
    updated_at: "",
    ...partial,
  };
}

describe("job lib", () => {
  it("formats stage and view like the workbench", () => {
    expect(stageViewLabel("executing", "headless")).toBe("实施中 · 无头");
    expect(stageViewLabel("plan_ready", "headed")).toBe("Plan 就绪 · 有头");
    expect(stageViewLabel("needs_input", "headless")).toBe("需要输入 · 无头");
  });

  it("formats elapsed minutes", () => {
    expect(formatElapsed(18 * 60)).toBe("18m");
  });

  it("filters by query, state, project and view", () => {
    const jobs = [
      job({ job_id: "1" }),
      job({ job_id: "2", title: "登录重构", project: "auth", state: "plan_ready", view_mode: "headed" }),
    ];
    expect(filterJobs(jobs, { query: "登录", state: "all", project: "all", view: "all" })).toHaveLength(1);
    expect(filterJobs(jobs, { query: "", state: "executing", project: "all", view: "all" })).toHaveLength(1);
    expect(filterJobs(jobs, { query: "", state: "all", project: "auth", view: "all" })).toHaveLength(1);
    expect(filterJobs(jobs, { query: "", state: "all", project: "all", view: "headed" })).toHaveLength(1);
  });

  it("summarizes working and needs-input counts", () => {
    const sum = summarizeStatus([
      job({ state: "executing" }),
      job({ state: "plan_ready" }),
      job({ state: "needs_input" }),
    ]);
    expect(sum.working).toBe(1);
    expect(sum.needsInput).toBe(2);
  });

  it("filters attention and working groups", () => {
    const jobs = [
      job({ job_id: "1", state: "executing" }),
      job({ job_id: "2", state: "plan_ready" }),
      job({ job_id: "3", state: "needs_input" }),
      job({ job_id: "4", state: "completed" }),
    ];
    expect(filterJobs(jobs, { query: "", state: "attention", project: "all", view: "all" }).map((j) => j.job_id)).toEqual(["2", "3"]);
    expect(filterJobs(jobs, { query: "", state: "working", project: "all", view: "all" }).map((j) => j.job_id)).toEqual(["1"]);
  });

  it("sorts needs-input before working", () => {
    const ordered = sortJobs([
      job({ job_id: "a", state: "completed", elapsed_seconds: 90 }),
      job({ job_id: "b", state: "executing", elapsed_seconds: 30 }),
      job({ job_id: "c", state: "needs_input", elapsed_seconds: 10 }),
      job({ job_id: "d", state: "plan_ready", elapsed_seconds: 20 }),
    ]);
    expect(ordered.map((j) => j.job_id)).toEqual(["c", "d", "b", "a"]);
  });
});
