import type { ButtonHTMLAttributes } from "react";
import { cn } from "../../lib/cn";

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "outline" | "ghost" | "danger";
  size?: "sm" | "md";
};

export function Button({ className, variant = "default", size = "md", ...props }: Props) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-1 rounded-md font-medium transition disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--green)]/40",
        size === "sm" ? "h-7 px-2 text-xs" : "h-8 px-3 text-sm",
        variant === "default" && "bg-[var(--green)] text-white hover:bg-[#145c3d]",
        variant === "outline" && "border border-[var(--line)] bg-white hover:bg-[var(--paper)]",
        variant === "ghost" && "hover:bg-[var(--green-soft)]",
        variant === "danger" && "bg-[var(--fail)] text-white hover:bg-[#8c323c]",
        className,
      )}
      {...props}
    />
  );
}
