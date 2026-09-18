import type { StatusBar } from "../api/client";
import { hideWindow, hostOS, isWails, minimiseWindow, toggleMaximiseWindow } from "../api/wails";

function Dot({ ok }: { ok: boolean }) {
  return <span className={`inline-block h-2 w-2 rounded-full ${ok ? "bg-[#58a17a]" : "bg-[#a33b45]"}`} />;
}

function WindowButtons() {
  const noDrag = { ["--wails-draggable" as string]: "no-drag" };
  return (
    <span className="ml-3 flex items-center gap-1" style={noDrag}>
      <button type="button" className="h-6 w-7 rounded text-[#edf6f0] hover:bg-white/15" aria-label="最小化" onClick={() => void minimiseWindow()}>–</button>
      <button type="button" className="h-6 w-7 rounded text-[#edf6f0] hover:bg-white/15" aria-label="最大化" onClick={() => void toggleMaximiseWindow()}>□</button>
      <button type="button" className="h-6 w-7 rounded text-[#edf6f0] hover:bg-[#a33b45]" aria-label="关闭" onClick={() => void hideWindow()}>×</button>
    </span>
  );
}

export function TitleBar({ bar }: { bar: StatusBar }) {
  const os = hostOS();
  const mac = isWails() && os === "darwin";
  const windows = isWails() && os === "windows";
  return (
    <header
      className={`flex h-11 shrink-0 select-none items-center justify-between bg-[var(--head)] pr-3 text-xs text-[#edf6f0] ${mac ? "pl-[78px]" : "pl-4"}`}
      style={{ ["--wails-draggable" as string]: "drag", WebkitUserSelect: "none" }}
    >
      <span className="font-semibold">Grok Supervisor</span>
      <span className="flex items-center gap-4">
        <span className="flex items-center gap-1"><Dot ok={bar.leader_ok} /> Leader</span>
        <span className="flex items-center gap-1"><Dot ok={bar.mcp_ok} /> MCP</span>
        <span>{bar.working} 工作　{bar.needs_input} 需输入</span>
        {windows ? <WindowButtons /> : null}
      </span>
    </header>
  );
}
