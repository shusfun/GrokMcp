import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { FolderPlus } from "lucide-react";
import { getClient } from "../api";
import { isWails } from "../api/wails";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { EmptyState, ErrorState } from "../components/EmptyState";
import { skillLabel, skillTone, sortProjects, type Project } from "../lib/projects";

async function pickDirectory(): Promise<string> {
  if (isWails()) {
    const { Dialogs } = await import("@wailsio/runtime");
    const path = await Dialogs.OpenFile({ CanChooseDirectories: true, CanChooseFiles: false, Title: "导入项目" });
    return typeof path === "string" ? path : "";
  }
  return window.prompt("项目路径") ?? "";
}

function lastUsedLabel(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "—";
  return new Date(t).toLocaleString();
}

export function ProjectsPage() {
  const client = getClient();
  const navigate = useNavigate();
  const [projects, setProjects] = useState<Project[]>([]);
  const [error, setError] = useState("");
  const [pendingPath, setPendingPath] = useState("");
  const [installSkill, setInstallSkill] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");

  const load = () => {
    client.listProjects().then((list) => {
      setProjects(sortProjects(list ?? []));
      setError("");
    }).catch((e: Error) => setError(e.message));
  };

  useEffect(() => {
    load();
    return client.subscribe(load);
  }, [client]);

  const startImport = () => {
    void pickDirectory().then((path) => {
      if (!path.trim()) return;
      setPendingPath(path);
      setInstallSkill(true);
    });
  };

  const confirmImport = () => {
    if (busy || !pendingPath) return;
    setBusy(true);
    client.importProject(pendingPath, installSkill).then((p) => {
      setPendingPath("");
      if (p.skill_status === "conflict") {
        setNotice(p.skill_message || "项目已登记，但 Skill 路径存在用户文件，未覆盖。");
      }
      load();
      navigate(`/projects/${p.project_id}`);
    }).catch((e: Error) => setError(e.message)).finally(() => setBusy(false));
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--row)]">
      <div className="flex items-center justify-between border-b border-[var(--line)] px-4 py-2">
        <h1 className="text-sm font-semibold">项目</h1>
        <Button size="sm" onClick={startImport}><FolderPlus className="h-3.5 w-3.5" />导入项目</Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-3">
        {error ? <ErrorState message={error} /> : null}
        {!error && projects.length === 0 ? <EmptyState title="没有项目" detail="导入一个仓库目录，或从 Codex 调用 grok_dispatch 后会出现已发现项目。" /> : null}
        <ul className="grid gap-2">
          {projects.map((p) => (
            <li key={p.project_id}>
              <button
                type="button"
                className="w-full rounded-md border border-[var(--line)] bg-[var(--surface)] px-3 py-3 text-left hover:border-[var(--accent)]"
                onClick={() => navigate(`/projects/${p.project_id}`)}
              >
                <span className="flex items-baseline justify-between gap-2">
                  <span className="truncate text-sm font-medium">{p.name}</span>
                  <span className="shrink-0 text-[11px] text-[var(--muted)]">{p.imported ? "已导入" : "已发现"}</span>
                </span>
                <span className="mt-1 block truncate text-xs text-[var(--muted)]">{p.root}</span>
                <span className="mt-2 flex flex-wrap items-center gap-2 text-xs text-[var(--muted)]">
                  <Badge tone={skillTone(p.skill_status)}>{skillLabel(p.skill_status)}</Badge>
                  <span>活动 {p.active_count}</span>
                  <span>需输入 {p.needs_input_count}</span>
                  <span>最近 {lastUsedLabel(p.last_used_at)}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      </div>
      <Dialog
        open={Boolean(pendingPath)}
        title="导入项目"
        onOpenChange={(open) => { if (!open) setPendingPath(""); }}
        footer={(
          <>
            <Button size="sm" variant="outline" onClick={() => setPendingPath("")}>取消</Button>
            <Button size="sm" disabled={busy} onClick={confirmImport}>导入</Button>
          </>
        )}
      >
        <p className="break-all text-[var(--ink)]">{pendingPath}</p>
        <label className="mt-3 flex items-center gap-2 text-[var(--ink)]">
          <input type="checkbox" checked={installSkill} onChange={(e) => setInstallSkill(e.target.checked)} />
          安装工具说明 Skill
        </label>
        <p className="mt-2 text-xs">默认安装。若同路径已有用户文件则不覆盖，项目仍会登记。</p>
      </Dialog>
      <Dialog open={Boolean(notice)} title="Skill 未覆盖" onOpenChange={(open) => { if (!open) setNotice(""); }}>
        {notice}
      </Dialog>
    </div>
  );
}
