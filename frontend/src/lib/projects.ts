export type SkillStatus = "missing" | "installed" | "outdated" | "conflict" | "error";

export type Project = {
  project_id: string;
  name: string;
  root: string;
  canonical_path: string;
  git_root?: string;
  imported: boolean;
  skill_status: SkillStatus;
  skill_version?: string;
  skill_message?: string;
  prompt_template?: string;
  created_at: string;
  updated_at: string;
  last_used_at: string;
  active_count: number;
  needs_input_count: number;
};

export type PromptResult = {
  project_id: string;
  text: string;
  template?: string;
  builtin: string;
  goal?: string;
  constraints?: string;
  acceptance?: string;
};

export function compareProjects(a: Project, b: Project): number {
  const aActive = a.active_count > 0 ? 1 : 0;
  const bActive = b.active_count > 0 ? 1 : 0;
  if (bActive !== aActive) return bActive - aActive;
  const lu = unixSeconds(b.last_used_at) - unixSeconds(a.last_used_at);
  if (lu !== 0) return lu;
  if (b.project_id === a.project_id) return 0;
  return b.project_id > a.project_id ? 1 : -1;
}

export function sortProjects(list: Project[]): Project[] {
  return [...list].sort(compareProjects);
}

export function projectDetailPath(projectId: string): string {
  return `/projects/${projectId}`;
}

export function persistentSkillMessage(project: Pick<Project, "skill_status" | "skill_message">): string {
  if (project.skill_status === "conflict") {
    return project.skill_message || "同路径已有用户文件，未覆盖。";
  }
  if (project.skill_status === "error") {
    return project.skill_message || "Skill 安装失败。";
  }
  return "";
}

export function skillLabel(status: SkillStatus | string): string {
  switch (status) {
    case "installed":
      return "Skill 已安装";
    case "outdated":
      return "Skill 可更新";
    case "conflict":
      return "Skill 冲突";
    case "error":
      return "Skill 出错";
    default:
      return "Skill 未安装";
  }
}

export function skillTone(status: SkillStatus | string): "ok" | "wait" | "fail" | "default" {
  switch (status) {
    case "installed":
      return "ok";
    case "outdated":
      return "wait";
    case "conflict":
    case "error":
      return "fail";
    default:
      return "default";
  }
}

function unixSeconds(iso: string): number {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return 0;
  return Math.floor(t / 1000);
}
