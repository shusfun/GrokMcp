import { Outlet, useNavigate } from "react-router";
import { useEffect, useState } from "react";
import { getClient } from "./api";
import type { StatusBar as StatusBarData } from "./api/client";
import { TitleBar } from "./components/TitleBar";
import { StatusBar } from "./components/StatusBar";
import { UpdateBar } from "./components/UpdateBar";
import { Dialog } from "./components/ui/dialog";
import { Button } from "./components/ui/button";
import { isWails, listenDesktop, quitApp } from "./api/wails";

const emptyBar: StatusBarData = { leader_ok: false, mcp_ok: false, working: 0, needs_input: 0 };

export function App() {
  const [quitWarn, setQuitWarn] = useState(false);
  const [bar, setBar] = useState<StatusBarData>(emptyBar);
  const navigate = useNavigate();
  const client = getClient();

  useEffect(() => {
    const load = () => {
      void client.statusBar().then(setBar).catch(() => setBar(emptyBar));
    };
    load();
    return client.subscribe(load);
  }, [client]);

  useEffect(() => {
    if (!isWails()) return;
    let stop = () => undefined as void;
    void listenDesktop({
      navigate: (hash) => {
        const path = hash.replace(/^#/, "") || "/";
        navigate(path);
      },
      quitWarn: () => setQuitWarn(true),
    }).then((off) => {
      stop = off;
    });
    return () => stop();
  }, [navigate]);

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--paper)] text-[var(--ink)]">
      <TitleBar />
      <UpdateBar />
      <main className="min-h-0 flex-1 overflow-hidden">
        <Outlet />
      </main>
      <StatusBar bar={bar} />
      <Dialog
        open={quitWarn}
        title="仍有活动任务"
        onOpenChange={setQuitWarn}
        footer={(
          <>
            <Button size="sm" variant="outline" onClick={() => setQuitWarn(false)}>留在托盘</Button>
            <Button size="sm" variant="danger" onClick={() => void quitApp()}>仍要退出</Button>
          </>
        )}
      >
        退出不会删除 Grok session，但会断开 Supervisor。活动任务仍在时请先取消或转为无头。
      </Dialog>
    </div>
  );
}
