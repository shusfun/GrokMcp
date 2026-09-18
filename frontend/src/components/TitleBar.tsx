import { LayoutDashboard, Moon, Monitor, Settings, Sun } from "lucide-react";
import { Link, useLocation } from "react-router";
import { getClient } from "../api";
import { hideWindow, hostOS, isWails, minimiseWindow, toggleMaximiseWindow } from "../api/wails";
import { cn } from "../lib/cn";
import type { Appearance } from "../lib/theme";
import { useTheme } from "./ThemeProvider";
import { Button } from "./ui/button";
import { DropdownItem, DropdownMenu } from "./ui/dropdown-menu";

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

const APPEARANCE_ITEMS: { value: Appearance; label: string; icon: typeof Sun }[] = [
  { value: "system", label: "跟随系统", icon: Monitor },
  { value: "light", label: "浅色", icon: Sun },
  { value: "dark", label: "深色", icon: Moon },
];

function ThemeToggle() {
  const { appearance, resolved, setAppearance } = useTheme();
  const Icon = resolved === "dark" ? Moon : Sun;
  return (
    <DropdownMenu
      trigger={(
        <Button size="icon" variant="ghost" aria-label="外观" title="外观">
          <Icon className="h-3.5 w-3.5" />
        </Button>
      )}
    >
      {APPEARANCE_ITEMS.map((item) => (
        <DropdownItem key={item.value} onClick={() => setAppearance(item.value)}>
          <span className={cn("flex items-center gap-2", appearance === item.value && "text-[var(--accent)]")}>
            <item.icon className="h-3.5 w-3.5" />
            {item.label}
          </span>
        </DropdownItem>
      ))}
    </DropdownMenu>
  );
}

export function TitleBar() {
  const os = hostOS();
  const mac = isWails() && os === "darwin";
  const windows = isWails() && os === "windows";
  const noDrag = { ["--wails-draggable" as string]: "no-drag" };
  const path = useLocation().pathname;
  const settingsOn = path.startsWith("/settings") || path.startsWith("/diagnostics");
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
          onClick={() => void getClient().openTerminal("", true)}
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
    </header>
  );
}
