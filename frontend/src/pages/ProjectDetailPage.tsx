import { useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import { getClient } from "../api";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Tabs } from "../components/ui/tabs";
import { ErrorState } from "../components/EmptyState";
import { WorkbenchPage } from "./WorkbenchPage";
import { persistentSkillMessage, skillLabel, skillTone, type Project, type PromptResult } from "../lib/projects";
import { builtinPromptTemplate, generateDraft } from "../lib/prompt";

const TABS = [
  { id: "jobs", label: "任务" },
  { id: "prompt", label: "提示词" },
  { id: "settings", label: "设置" },
];

export function ProjectDetailPage() {
  const { projectId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = TABS.some((t) => t.id === params.get("tab")) ? params.get("tab")! : "jobs";
  const client = getClient();
  const [project, setProject] = useState<Project | null>(null);
  const [error, setError] = useState("");

  const load = () => {
    client.getProject(projectId).then((p) => {
      setProject(p);
      setError("");
    }).catch((e: Error) => setError(e.message));
  };

  useEffect(() => {
    load();
    return client.subscribe(load);
  }, [client, projectId]);

  if (error) return <ErrorState message={error} />;
  if (!project) return <div className="p-4 text-sm text-[var(--muted)]">加载中…</div>;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="shrink-0 border-b border-[var(--line)] bg-[var(--row)] px-4 py-3">
        <Link to="/" className="text-xs text-[var(--muted)] hover:text-[var(--accent)]">← 项目总览</Link>
        <div className="mt-1 flex flex-wrap items-baseline gap-2">
          <h1 className="text-base font-semibold">{project.name}</h1>
          <span className="text-xs text-[var(--muted)]">{project.imported ? "已导入" : "已发现"}</span>
          <Badge tone={skillTone(project.skill_status)}>{skillLabel(project.skill_status)}</Badge>
        </div>
        <p className="mt-1 truncate text-xs text-[var(--muted)]">{project.root}</p>
        {persistentSkillMessage(project) ? (
          <p className="mt-1 text-xs text-[var(--fail)]" data-testid="skill-alert">{persistentSkillMessage(project)}</p>
        ) : null}
      </header>
      <div className="shrink-0 bg-[var(--row)] px-2">
        <Tabs tabs={TABS} value={tab} onChange={(id) => setParams(id === "jobs" ? {} : { tab: id })} />
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        {tab === "jobs" ? <WorkbenchPage projectId={project.project_id} listOnly /> : null}
        {tab === "prompt" ? <PromptPane project={project} onChange={setProject} /> : null}
        {tab === "settings" ? <SettingsPane project={project} onChange={setProject} /> : null}
      </div>
    </div>
  );
}

function PromptPane({ project, onChange }: { project: Project; onChange: (p: Project) => void }) {
  const client = getClient();
  const [template, setTemplate] = useState(project.prompt_template || builtinPromptTemplate);
  const [draft, setDraft] = useState("");
  const [builtin, setBuiltin] = useState(builtinPromptTemplate);
  const [goal, setGoal] = useState("");
  const [constraints, setConstraints] = useState("");
  const [acceptance, setAcceptance] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);

  const apply = (out: PromptResult) => {
    setTemplate(out.template || builtinPromptTemplate);
    setDraft(out.text);
    if (out.builtin) setBuiltin(out.builtin);
  };

  useEffect(() => {
    void client.generatePrompt(project.project_id, goal, constraints, acceptance).then(apply);
  }, [client, project.project_id]);

  const run = (fn: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    fn().catch((e: Error) => setNotice(e.message)).finally(() => setBusy(false));
  };

  return (
    <div className="h-full overflow-auto p-4">
      <p className="mb-3 text-xs text-[var(--muted)]">只用于复制到 Codex，不会自动发送或注入会话。保存默认会保留模板占位符；重新生成使用已保存模板。</p>
      <label className="mb-3 block text-xs text-[var(--muted)]">项目默认模板
        <textarea className="mt-1 min-h-36 w-full rounded-md border border-[var(--line)] bg-[var(--surface)] p-3 font-mono text-xs text-[var(--ink)]" value={template} onChange={(e) => setTemplate(e.target.value)} />
      </label>
      <div className="mb-3 grid gap-2">
        <label className="text-xs text-[var(--muted)]">本次让 Grok 完成
          <textarea className="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--surface)] p-2 text-sm text-[var(--ink)]" rows={2} value={goal} onChange={(e) => setGoal(e.target.value)} />
        </label>
        <label className="text-xs text-[var(--muted)]">约束
          <textarea className="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--surface)] p-2 text-sm text-[var(--ink)]" rows={2} value={constraints} onChange={(e) => setConstraints(e.target.value)} />
        </label>
        <label className="text-xs text-[var(--muted)]">验收
          <textarea className="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--surface)] p-2 text-sm text-[var(--ink)]" rows={2} value={acceptance} onChange={(e) => setAcceptance(e.target.value)} />
        </label>
      </div>
      <label className="block text-xs text-[var(--muted)]">本次提示词
        <textarea className="mt-1 min-h-40 w-full rounded-md border border-[var(--line)] bg-[var(--surface)] p-3 font-mono text-xs text-[var(--ink)]" value={draft} onChange={(e) => setDraft(e.target.value)} />
      </label>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button size="sm" disabled={busy} onClick={() => void navigator.clipboard.writeText(draft).then(() => setNotice("已复制"))}>复制</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => run(async () => { apply(await client.generatePrompt(project.project_id, goal, constraints, acceptance)); })}>重新生成</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => run(async () => { onChange(await client.savePrompt(project.project_id, template)); setNotice("已保存为项目默认模板"); })}>保存为项目默认</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => {
          setTemplate(builtin);
          setDraft(generateDraft(builtin, { name: project.name, root: project.root, goal, constraints, acceptance }));
          setNotice("已恢复内置模板，未保存");
        }}>恢复内置模板</Button>
      </div>
      {notice ? <p className="mt-2 text-xs text-[var(--muted)]">{notice}</p> : null}
    </div>
  );
}

function SettingsPane({ project, onChange }: { project: Project; onChange: (p: Project) => void }) {
  const client = getClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirmRemove, setConfirmRemove] = useState(false);

  const run = (fn: () => Promise<Project>) => {
    if (busy) return;
    setBusy(true);
    setError("");
    fn().then(onChange).catch((e: Error) => setError(e.message)).finally(() => setBusy(false));
  };

  return (
    <div className="h-full overflow-auto p-4 text-sm">
      <p className="text-xs text-[var(--muted)]">路径</p>
      <p className="mt-1 break-all">{project.root}</p>
      {project.git_root ? <p className="mt-1 text-xs text-[var(--muted)]">Git 根：{project.git_root}</p> : null}
      <div className="mt-4 flex flex-wrap gap-2">
        <Button size="sm" variant="outline" onClick={() => void client.openProjectDir(project.project_id)}>打开目录</Button>
        <Button size="sm" disabled={busy} onClick={() => run(() => client.skillInstall(project.project_id))}>安装 Skill</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => client.skillUpdate(project.project_id))}>更新 Skill</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => client.skillRemove(project.project_id))}>移除 Skill</Button>
      </div>
      <p className="mt-2 text-xs text-[var(--muted)]">{skillLabel(project.skill_status)}</p>
      {persistentSkillMessage(project) ? <p className="mt-1 text-xs text-[var(--fail)]">{persistentSkillMessage(project)}</p> : null}
      <div className="mt-8 border-t border-[var(--line)] pt-4">
        <p className="text-sm font-medium">移除项目</p>
        <p className="mt-1 text-xs text-[var(--muted)]">只把项目降为已发现，不删除源码、历史任务、Grok session 或 Skill。</p>
        <Button size="sm" variant="danger" className="mt-3" disabled={busy || !project.imported} onClick={() => setConfirmRemove(true)}>
          {project.imported ? "降为已发现" : "已是已发现项目"}
        </Button>
      </div>
      {error ? <p className="mt-3 text-xs text-[var(--fail)]">{error}</p> : null}
      <Dialog
        open={confirmRemove}
        title="移除项目登记"
        onOpenChange={setConfirmRemove}
        footer={(
          <>
            <Button size="sm" variant="outline" onClick={() => setConfirmRemove(false)}>取消</Button>
            <Button size="sm" variant="danger" onClick={() => { setConfirmRemove(false); run(() => client.removeProject(project.project_id)); }}>降为已发现</Button>
          </>
        )}
      >
        不会删除源码目录、历史任务或 ~/.grok/sessions。Skill 文件保留，需要时请用「移除 Skill」。
      </Dialog>
    </div>
  );
}
