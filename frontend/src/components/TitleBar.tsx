import { useState } from "react";
import { LayoutDashboard, Moon, Settings, Sun } from "lucide-react";
import { Link, useLocation } from "react-router";
import { getClient } from "../api";
import { hideWindow, hostOS, isWails, minimiseWindow, toggleMaximiseWindow } from "../api/wails";
import { cn } from "../lib/cn";
import { toggleResolved } from "../lib/theme";
import { useTheme } from "./ThemeProvider";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";

function WindowButtons() {
  const noDrag = { ["--wails-draggable" as string]: "no-drag" };
  return (
    <span className="ml-1 flex items-center gap-0.5" style={noDrag}>
      <button type="button" className="h-6 w-7 rounded text-[var(--ink)] hover:bg-[var(--accent-soft)]" aria-label="最小化" onClick={() => void minimiseWindow()}>–</button>
      <button type="button" className="h-6 w-7 rounded text-[var(--ink)] hover:bg-[var(--accent-soft)]" aria-label="最大化" onClick={() => void toggleMaximiseWindow()}>□</button>
      <button type="button" className="h-6 w-7 rounded text-[var(--ink)] hover:bg-[var(--fail-soft)] hover:text-[var(--fail)]" aria-label="关闭" onClick={() => void hideWindow()}>×</button>
    </span>
  );
}

function ThemeToggle() {
  const { resolved, setAppearance } = useTheme();
  const Icon = resolved === "dark" ? Moon : Sun;
  const next = resolved === "dark" ? "浅色" : "深色";
  return (
    <Button
      size="icon"
      variant="ghost"
      aria-label={`切换${next}`}
      title={`切换${next}`}
      onClick={() => setAppearance(toggleResolved(resolved))}
    >
      <Icon className="h-3.5 w-3.5" />
    </Button>
  );
}

export function TitleBar() {
  const os = hostOS();
  const mac = isWails() && os === "darwin";
  const windows = isWails() && os === "windows";
  const noDrag = { ["--wails-draggable" as string]: "no-drag" };
  const path = useLocation().pathname;
  const settingsOn = path.startsWith("/settings") || path.startsWith("/diagnostics");
  const [dashError, setDashError] = useState("");
  return (
    <header
      className={`flex h-11 shrink-0 select-none items-center justify-between border-b border-[var(--line)] bg-[var(--head)] pr-2 text-sm ${mac ? "pl-[78px]" : "pl-3"}`}
      style={{ ["--wails-draggable" as string]: "drag", WebkitUserSelect: "none" }}
    >
      <Link to="/" className="font-semibold hover:text-[var(--accent)]">Grok Supervisor</Link>
      <span className="flex items-center gap-0.5" style={noDrag}>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            void getClient().openTerminal("", true).catch((e: Error) => setDashError(e.message || "无法打开 Dashboard"));
          }}
        >
          <LayoutDashboard className="h-3.5 w-3.5" />
          Dashboard
        </Button>
        <ThemeToggle />
        <Link
          to="/settings"
          title="设置"
          className={cn(
            "inline-flex h-7 w-7 items-center justify-center rounded-md hover:bg-[var(--accent-soft)]",
            settingsOn && "bg-[var(--accent-soft)] text-[var(--accent)]",
          )}
        >
          <Settings className="h-3.5 w-3.5" />
        </Link>
        {windows ? <WindowButtons /> : null}
      </span>
      <Dialog open={Boolean(dashError)} title="无法打开 Dashboard" onOpenChange={(open) => { if (!open) setDashError(""); }}>
        {dashError}
      </Dialog>
    </header>
  );
}
