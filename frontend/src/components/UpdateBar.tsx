import { useEffect, useState } from "react";
import { getClient } from "../api";
import { isWails, listenUpdater } from "../api/wails";
import { Button } from "./ui/button";

type Phase = "idle" | "checking" | "available" | "downloading" | "verifying" | "installing" | "ready" | "latest" | "error";

function percent(written: number, total: number) {
  if (total <= 0) return 0;
  return Math.min(100, Math.round((written / total) * 100));
}

const HIDDEN: Phase[] = ["idle", "checking", "latest"];

export function UpdateBar() {
  const client = getClient();
  const [latest, setLatest] = useState("");
  const [phase, setPhase] = useState<Phase>("idle");
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!isWails()) return;
    let stop = () => undefined as void;
    void listenUpdater({
      checkStarted: () => setPhase("checking"),
      updateAvailable: (ver) => {
        setLatest(ver);
        setPhase("available");
      },
      noUpdate: () => setPhase("latest"),
      downloadStarted: () => {
        setProgress(0);
        setPhase("downloading");
      },
      downloadProgress: (written, total) => {
        setProgress(percent(written, total));
        setPhase("downloading");
      },
      verifying: () => setPhase("verifying"),
      installing: () => setPhase("installing"),
      updateReady: () => setPhase("ready"),
      error: (msg) => {
        setError(msg);
        setPhase("error");
      },
    }).then((off) => {
      stop = off;
    });
    return () => stop();
  }, []);

  if (HIDDEN.includes(phase)) return null;

  const busy = phase === "downloading" || phase === "verifying" || phase === "installing";

  return (
    <div className="relative shrink-0 border-b border-[var(--line)] bg-[var(--accent-soft)] px-3 py-1.5 text-xs text-[var(--ink)]" style={{ ["--wails-draggable" as string]: "no-drag" }}>
      <div className="flex items-center justify-between gap-3">
        <span className="min-w-0 truncate">
          {phase === "available" ? `发现新版本 ${latest}` : null}
          {phase === "downloading" ? `正在下载 ${progress}%` : null}
          {phase === "verifying" ? "正在校验…" : null}
          {phase === "installing" ? "正在安装…" : null}
          {phase === "ready" ? "更新已就绪，重启后生效" : null}
          {phase === "error" ? error || "更新失败" : null}
        </span>
        <span className="flex shrink-0 items-center gap-2">
          {phase === "available" ? (
            <Button size="sm" disabled={busy} onClick={() => void client.downloadUpdate()}>下载安装</Button>
          ) : null}
          {phase === "ready" ? (
            <Button size="sm" onClick={() => void client.restartUpdate()}>重启</Button>
          ) : null}
        </span>
      </div>
      {phase === "downloading" ? (
        <div className="absolute inset-x-0 bottom-0 h-0.5 bg-[var(--line)]">
          <div className="h-full bg-[var(--accent)]" style={{ width: `${progress}%` }} />
        </div>
      ) : null}
    </div>
  );
}
