import { useEffect, useRef, useState } from "react";
import { cn } from "../../lib/cn";

export type SelectOption = { value: string; label: string };

export function Select({
  className,
  value,
  options,
  onChange,
  "aria-label": ariaLabel,
}: {
  className?: string;
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  "aria-label"?: string;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const label = options.find((opt) => opt.value === value)?.label ?? value;

  useEffect(() => {
    if (!open) return;
    const onDoc = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  return (
    <div ref={root} className={cn("relative", className)}>
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
        className="flex h-8 w-full items-center justify-between gap-2 rounded-md border border-[var(--line)] bg-white px-2 text-left text-sm text-[var(--ink)]"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="truncate">{label}</span>
        <span className="text-[#607068]">▾</span>
      </button>
      {open ? (
        <ul
          role="listbox"
          className="absolute z-30 mt-1 max-h-60 w-full overflow-auto rounded-md border border-[var(--line)] bg-white py-1 shadow"
        >
          {options.map((opt) => (
            <li key={opt.value}>
              <button
                type="button"
                role="option"
                aria-selected={opt.value === value}
                className={cn(
                  "block w-full px-3 py-1.5 text-left text-sm hover:bg-[var(--green-soft)]",
                  opt.value === value && "font-semibold text-[var(--green)]",
                )}
                onClick={() => {
                  onChange(opt.value);
                  setOpen(false);
                }}
              >
                {opt.label}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
