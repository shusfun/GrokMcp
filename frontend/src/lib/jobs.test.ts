import { describe, expect, it } from "vitest";
import { canArchive, canControlTurn, canDelete, dashboardActive, dashboardHistory, filterJobs, formatElapsed, mergeJobs, sortJobs, stageViewLabel, summarizeStatus, type Job } from "./jobs";

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
    expect(filterJobs(jobs, { query: "登录", state: "all", project: "all", view: "all", include_archived: false })).toHaveLength(1);
    expect(filterJobs(jobs, { query: "", state: "executing", project: "all", view: "all", include_archived: false })).toHaveLength(1);
    expect(filterJobs(jobs, { query: "", state: "all", project: "auth", view: "all", include_archived: false })).toHaveLength(1);
    expect(filterJobs(jobs, { query: "", state: "all", project: "all", view: "headed", include_archived: false })).toHaveLength(1);
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
    expect(filterJobs(jobs, { query: "", state: "attention", project: "all", view: "all", include_archived: false }).map((j) => j.job_id)).toEqual(["2", "3"]);
    expect(filterJobs(jobs, { query: "", state: "working", project: "all", view: "all", include_archived: false }).map((j) => j.job_id)).toEqual(["1"]);
  });

  it("sorts active before history then updated_at created_at job_id", () => {
    const ordered = sortJobs([
      job({ job_id: "z", state: "executing", updated_at: "1970-01-01T00:01:40Z", created_at: "1970-01-01T00:00:50Z" }),
      job({ job_id: "a", state: "completed", updated_at: "1970-01-01T00:03:20Z", created_at: "1970-01-01T00:00:10Z" }),
      job({ job_id: "m", state: "needs_input", updated_at: "1970-01-01T00:01:30Z", created_at: "1970-01-01T00:01:20Z" }),
      job({ job_id: "c", state: "created", updated_at: "1970-01-01T00:05:00Z", created_at: "1970-01-01T00:05:00Z" }),
      job({ job_id: "f", state: "failed", updated_at: "1970-01-01T00:04:10Z", created_at: "1970-01-01T00:00:20Z" }),
      job({ job_id: "y", state: "executing", updated_at: "1970-01-01T00:01:40Z", created_at: "1970-01-01T00:01:00Z" }),
      job({ job_id: "x", state: "executing", updated_at: "1970-01-01T00:01:40Z", created_at: "1970-01-01T00:01:00Z" }),
    ]);
    expect(ordered.map((j) => j.job_id)).toEqual(["y", "x", "z", "m", "c", "f", "a"]);
  });

  it("hides turn controls after cancel or finish", () => {
    expect(canControlTurn(job({ state: "needs_input" }))).toBe(true);
    expect(canControlTurn(job({ state: "executing" }))).toBe(true);
    expect(canControlTurn(job({ state: "plan_ready" }))).toBe(false);
    expect(canControlTurn(job({ state: "cancelled" }))).toBe(false);
    expect(canControlTurn(job({ state: "completed" }))).toBe(false);
    expect(canControlTurn(job({ state: "failed" }))).toBe(false);
    expect(canControlTurn(job({ state: "needs_input", user_cancelled: true }))).toBe(false);
  });

  it("does not treat created as active or history", () => {
    expect(dashboardActive("created")).toBe(false);
    expect(dashboardHistory("created")).toBe(false);
    expect(canArchive(job({ state: "created" }))).toBe(true);
    expect(canDelete(job({ state: "executing" }))).toBe(false);
    expect(canArchive(job({ state: "needs_input" }))).toBe(false);
  });

  it("merges pages by job id then resorts", () => {
    const merged = mergeJobs(
      [job({ job_id: "z", state: "executing", updated_at: "1970-01-01T00:01:40Z", created_at: "1970-01-01T00:00:50Z" })],
      [job({ job_id: "a", state: "completed", updated_at: "1970-01-01T00:03:20Z", created_at: "1970-01-01T00:00:10Z" })],
    );
    expect(merged.map((j) => j.job_id)).toEqual(["z", "a"]);
  });
});
