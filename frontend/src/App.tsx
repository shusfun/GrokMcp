import { NavLink, Outlet, useNavigate } from "react-router";
import { useEffect, useState } from "react";
import { getClient } from "./api";
import type { StatusBar } from "./api/client";
import { TitleBar } from "./components/TitleBar";
import { Dialog } from "./components/ui/dialog";
import { Button } from "./components/ui/button";
import { isWails, listenDesktop, quitApp } from "./api/wails";
import { cn } from "./lib/cn";

const emptyBar: StatusBar = { leader_ok: false, mcp_ok: false, working: 0, needs_input: 0 };

const TABS = [
  { to: "/", label: "总览", end: true },
  { to: "/settings", label: "设置", end: false },
  { to: "/diagnostics", label: "诊断", end: false },
];

export function App() {
  const [quitWarn, setQuitWarn] = useState(false);
  const [bar, setBar] = useState<StatusBar>(emptyBar);
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
      <TitleBar bar={bar} />
      <div className="flex min-h-0 flex-1 flex-col bg-[var(--surface)]">
        <nav className="flex gap-1 border-b border-[var(--line)] px-3" style={{ ["--wails-draggable" as string]: "no-drag" }}>
          {TABS.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={tab.end}
              className={({ isActive }) => cn(
                "-mb-px border-b-2 px-3 py-2 text-sm",
                isActive
                  ? "border-[var(--green)] font-semibold text-[var(--green)]"
                  : "border-transparent text-[var(--muted)] hover:text-[var(--ink)]",
              )}
            >
              {tab.label}
            </NavLink>
          ))}
        </nav>
        <main className="min-h-0 flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
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
