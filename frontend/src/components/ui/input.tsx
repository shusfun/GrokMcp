import type { InputHTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn("h-8 w-full rounded-md border border-[var(--line)] bg-white px-2 text-sm text-[var(--ink)] outline-none focus:border-[var(--green)]", className)}
      {...props}
    />
  );
}
