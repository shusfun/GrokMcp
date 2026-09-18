import { useState } from "react";
import type { Job } from "../lib/jobs";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

type Props = {
  job: Job;
  onShowTui: () => void;
  onHeadless: () => void;
  onCancel: () => void;
  onContinue: () => void;
  onOpenDir: () => void;
  onPlanDecide?: (decide: "approve" | "revise" | "cancel", notes: string) => void;
};

export function JobActions({ job, onShowTui, onHeadless, onCancel, onContinue, onOpenDir, onPlanDecide }: Props) {
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const planReady = job.state === "plan_ready";
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
      <div className="flex flex-wrap gap-2">
        {job.view_mode === "headed" ? (
          <Button size="sm" variant="outline" disabled={busy} onClick={() => run(onHeadless)}>转为无头</Button>
        ) : (
          <Button size="sm" disabled={busy} onClick={() => run(onShowTui)}>显示 TUI</Button>
        )}
        {!planReady ? (
          <>
            <Button size="sm" variant="danger" disabled={busy} onClick={() => run(onCancel)}>取消当前 turn</Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => run(onContinue)}>继续</Button>
          </>
        ) : null}
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => run(onOpenDir)}>打开项目目录</Button>
        <Button size="sm" variant="ghost" onClick={() => void navigator.clipboard.writeText(job.grok_session_id ?? "")}>复制 session ID</Button>
      </div>
      {error ? <p className="text-sm text-[var(--fail)]">{error}</p> : null}
    </div>
  );
}
