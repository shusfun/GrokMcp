import type { ReactNode } from "react";
import { useState } from "react";
import { Button } from "./button";

export function DropdownMenu({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <Button size="sm" variant="outline" onClick={() => setOpen((v) => !v)}>{label}</Button>
      {open ? (
        <div className="absolute right-0 z-20 mt-1 min-w-40 rounded-md border border-[#ccd8d1] bg-white py-1 shadow">
          <div onClick={() => setOpen(false)}>{children}</div>
        </div>
      ) : null}
    </div>
  );
}

export function DropdownItem({ children, onClick }: { children: ReactNode; onClick?: () => void }) {
  return (
    <button className="block w-full px-3 py-1.5 text-left text-sm hover:bg-[#e7f2eb]" onClick={onClick}>
      {children}
    </button>
  );
}
