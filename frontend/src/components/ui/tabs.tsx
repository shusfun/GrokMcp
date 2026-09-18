import { cn } from "../../lib/cn";

export function Tabs({ tabs, value, onChange }: { tabs: { id: string; label: string }[]; value: string; onChange: (id: string) => void }) {
  return (
    <div className="flex gap-1 border-b border-[var(--line)]">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          className={cn(
            "px-3 py-1.5 text-sm",
            value === tab.id ? "border-b-2 border-[var(--accent)] font-semibold text-[var(--accent)]" : "text-[var(--muted)]",
          )}
          onClick={() => onChange(tab.id)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}
