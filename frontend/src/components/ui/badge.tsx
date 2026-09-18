import type { HTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export function Badge({ className, tone = "default", ...props }: HTMLAttributes<HTMLSpanElement> & { tone?: "default" | "ok" | "wait" | "fail" }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded border px-1.5 py-0.5 text-[11px] font-semibold",
        tone === "default" && "border-[var(--line)] bg-[var(--surface)] text-[var(--muted)]",
        tone === "ok" && "border-transparent bg-[var(--green-soft)] text-[var(--green)]",
        tone === "wait" && "border-transparent bg-[var(--wait-soft)] text-[var(--wait)]",
        tone === "fail" && "border-transparent bg-[var(--fail-soft)] text-[var(--fail)]",
        className,
      )}
      {...props}
    />
  );
}
