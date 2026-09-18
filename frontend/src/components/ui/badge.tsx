import type { HTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export function Badge({ className, tone = "default", ...props }: HTMLAttributes<HTMLSpanElement> & { tone?: "default" | "ok" | "wait" | "fail" }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded border px-1.5 py-0.5 text-[11px] font-semibold",
        tone === "default" && "border-[#b9c9c0] bg-white text-[#3f5147]",
        tone === "ok" && "border-[#77aa8b] bg-[#e7f2eb] text-[#176b48]",
        tone === "wait" && "border-[#d9b76b] bg-[#fff4d9] text-[#9a6200]",
        tone === "fail" && "border-[#d6a1a7] bg-[#fae9eb] text-[#a33b45]",
        className,
      )}
      {...props}
    />
  );
}
