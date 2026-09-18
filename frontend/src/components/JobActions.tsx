import { useState } from "react";
import { Copy, FolderOpen, MoreHorizontal } from "lucide-react";
import { canControlTurn, type Job } from "../lib/jobs";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { DropdownItem, DropdownMenu } from "./ui/dropdown-menu";

type Props = {
  job: Job;
  onShowTui: () => void;
  onHeadless: () => void;
  onCancel: () => void;
  onContinue: () => void;
  onOpenDir: () => void;
  onPlanDecide?: (decide: "approve" | "revise" | "cancel", notes: string) => void;
  onDebug?: () => void;
  onTrace?: () => void;
  onExport?: () => void;
  onCopyState?: () => void;
};

export function JobActions({ job, onShowTui, onHeadless, onCancel, onContinue, onOpenDir, onPlanDecide, onDebug, onTrace, onExport, onCopyState }: Props) {
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const planReady = job.state === "plan_ready";
  const turnControl = canControlTurn(job);
  const run = (fn: () => void | Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setError("");
    Promise.resolve(fn()).catch((e: Error) => setError(e.message)).finally(() => setBusy(false));
  };
  return (
    <div className="space-y-3">
      {planReady && onPlanDecide ? (
        <div className="space-y-2">
          <Input value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="审批备注（可选）" />
          <div className="flex flex-wrap gap-2">
            <Button size="sm" disabled={busy} onClick={() => run(() => onPlanDecide("approve", notes))}>批准</Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => onPlanDecide("revise", notes))}>退回</Button>
            <Button size="sm" variant="danger" disabled={busy} onClick={() => run(() => onPlanDecide("cancel", notes))}>取消任务</Button>
          </div>
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {job.view_mode === "headed" ? (
          <Button size="sm" variant="outline" disabled={busy} onClick={() => run(onHeadless)}>转为无头</Button>
        ) : (
          <Button size="sm" disabled={busy} onClick={() => run(onShowTui)}>显示 TUI</Button>
        )}
        {onDebug ? (
          <Button size="sm" variant={job.debug_enabled ? "outline" : "default"} disabled={busy} onClick={() => run(onDebug)}>
            {job.debug_enabled ? "关闭调试" : "开启调试"}
          </Button>
        ) : null}
        {turnControl ? (
          <>
            <Button size="sm" variant="danger" disabled={busy} onClick={() => run(onCancel)}>取消当前 turn</Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => run(onContinue)}>继续</Button>
          </>
        ) : null}
        <DropdownMenu
          trigger={(
            <Button size="icon" variant="ghost" aria-label="更多" disabled={busy}>
              <MoreHorizontal className="h-4 w-4" />
            </Button>
          )}
        >
          <DropdownItem onClick={() => run(onOpenDir)}>
            <span className="flex items-center gap-2"><FolderOpen className="h-3.5 w-3.5" />打开项目目录</span>
          </DropdownItem>
          <DropdownItem onClick={() => void navigator.clipboard.writeText(job.grok_session_id ?? "")}>
            <span className="flex items-center gap-2"><Copy className="h-3.5 w-3.5" />复制 session ID</span>
          </DropdownItem>
          {onTrace ? <DropdownItem onClick={() => run(onTrace)}>查看 Trace</DropdownItem> : null}
          {onExport ? <DropdownItem onClick={() => run(onExport)}>导出诊断包</DropdownItem> : null}
          {onCopyState ? <DropdownItem onClick={() => run(onCopyState)}>复制内部状态</DropdownItem> : null}
        </DropdownMenu>
      </div>
      {error ? <p className="text-sm text-[var(--fail)]">{error}</p> : null}
    </div>
  );
}
