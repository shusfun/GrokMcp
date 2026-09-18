import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import { cn } from "../../lib/cn";
import { Button } from "./button";

export function DropdownMenu({
  label,
  trigger,
  align = "right",
  children,
}: {
  label?: string;
  trigger?: ReactNode;
  align?: "left" | "right";
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  return (
    <div ref={root} className="relative">
      {trigger ? (
        <span onClick={() => setOpen((v) => !v)}>{trigger}</span>
      ) : (
        <Button size="sm" variant="outline" onClick={() => setOpen((v) => !v)}>{label}</Button>
      )}
      {open ? (
        <div
          className={cn(
            "absolute z-20 mt-1 min-w-40 rounded-md border border-[var(--line)] bg-[var(--surface)] py-1 shadow-lg",
            align === "right" ? "right-0" : "left-0",
          )}
        >
          <div onClick={() => setOpen(false)}>{children}</div>
        </div>
      ) : null}
    </div>
  );
}

export function DropdownItem({ children, onClick }: { children: ReactNode; onClick?: () => void }) {
  return (
    <button
      type="button"
      className="block w-full px-3 py-1.5 text-left text-sm hover:bg-[var(--accent-soft)]"
      onClick={onClick}
    >
      {children}
    </button>
  );
}
