import { cn } from "../../lib/cn";

export function Segmented({
  value,
  options,
  onChange,
  "aria-label": ariaLabel,
}: {
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
  "aria-label"?: string;
}) {
  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className="inline-flex rounded-md border border-[var(--line)] bg-[var(--row)] p-0.5"
    >
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          role="radio"
          aria-checked={opt.value === value}
          className={cn(
            "h-7 rounded px-2.5 text-xs font-medium",
            opt.value === value
              ? "bg-[var(--accent)] text-white"
              : "text-[var(--muted)] hover:text-[var(--ink)]",
          )}
          onClick={() => onChange(opt.value)}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}
