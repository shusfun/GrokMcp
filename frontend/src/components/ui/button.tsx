import type { ButtonHTMLAttributes } from "react";
import { cn } from "../../lib/cn";

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "outline" | "ghost" | "danger";
  size?: "sm" | "md" | "icon";
};

export function Button({ className, variant = "default", size = "md", ...props }: Props) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-1 rounded-md font-medium transition disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]/40",
        size === "sm" && "h-7 px-2 text-xs",
        size === "md" && "h-8 px-3 text-sm",
        size === "icon" && "h-7 w-7 p-0",
        variant === "default" && "bg-[var(--accent)] text-white hover:brightness-110",
        variant === "outline" && "border border-[var(--line)] bg-[var(--surface)] text-[var(--ink)] hover:bg-[var(--row)]",
        variant === "ghost" && "text-[var(--ink)] hover:bg-[var(--accent-soft)]",
        variant === "danger" && "bg-[var(--fail)] text-white hover:brightness-110",
        className,
      )}
      {...props}
    />
  );
}
