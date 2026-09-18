import { cn } from "../../lib/cn";

export function Tabs({ tabs, value, onChange }: { tabs: { id: string; label: string }[]; value: string; onChange: (id: string) => void }) {
  return (
    <div className="flex gap-1 border-b border-[#ccd8d1]">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          className={cn(
            "px-3 py-1.5 text-sm",
            value === tab.id ? "border-b-2 border-[#176b48] font-semibold text-[#176b48]" : "text-[#607068]",
          )}
          onClick={() => onChange(tab.id)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}
