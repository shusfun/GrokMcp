import type { ReactNode } from "react";

export function Tooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span title={label} className="inline-flex">
      {children}
    </span>
  );
}
