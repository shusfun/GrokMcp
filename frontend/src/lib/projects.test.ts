import { describe, expect, it } from "vitest";
import { compareProjects, sortProjects, type Project } from "./projects";

function project(partial: Partial<Project>): Project {
  return {
    project_id: "p1",
    name: "n",
    root: "/tmp/n",
    canonical_path: "/tmp/n",
    imported: true,
    skill_status: "missing",
    created_at: "1970-01-01T00:00:00Z",
    updated_at: "1970-01-01T00:00:00Z",
    last_used_at: "1970-01-01T00:00:00Z",
    active_count: 0,
    needs_input_count: 0,
    ...partial,
  };
}

describe("projects", () => {
  it("sorts active first then last_used_at desc", () => {
    const ordered = sortProjects([
      project({ project_id: "old", active_count: 0, last_used_at: "1970-01-01T00:08:20Z" }),
      project({ project_id: "live", active_count: 2, last_used_at: "1970-01-01T00:00:10Z" }),
      project({ project_id: "mid", active_count: 0, last_used_at: "1970-01-01T00:01:00Z" }),
    ]);
    expect(ordered.map((p) => p.project_id)).toEqual(["live", "old", "mid"]);
  });

  it("compare treats equal ids as 0", () => {
    const a = project({ project_id: "x", active_count: 1 });
    expect(compareProjects(a, a)).toBe(0);
  });
});
