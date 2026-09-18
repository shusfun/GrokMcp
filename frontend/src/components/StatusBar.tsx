import { useEffect, useState } from "react";
import { Link } from "react-router";
import { getClient } from "../api";
import type { StatusBar as StatusBarData } from "../api/client";

function Dot({ ok }: { ok: boolean }) {
  return <span className={`inline-block h-1.5 w-1.5 rounded-full ${ok ? "bg-[var(--green)]" : "bg-[var(--fail)]"}`} />;
}

export function StatusBar({ bar }: { bar: StatusBarData }) {
  const [version, setVersion] = useState("");
  useEffect(() => {
    void getClient().appVersion().then(setVersion).catch(() => setVersion(""));
  }, []);

  return (
    <footer className="flex h-7 shrink-0 items-center justify-between border-t border-[var(--line)] bg-[var(--head)] px-3 text-[11px] text-[var(--muted)]">
      <Link to="/diagnostics" className="flex items-center gap-3 hover:text-[var(--ink)]">
        <span className="flex items-center gap-1.5"><Dot ok={bar.leader_ok} /> Leader</span>
        <span className="flex items-center gap-1.5"><Dot ok={bar.mcp_ok} /> MCP</span>
      </Link>
      <span className="tabular-nums">{version}</span>
    </footer>
  );
}
