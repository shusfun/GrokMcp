import type { ReactNode } from "react";
import { Button } from "./button";

type Props = {
  open: boolean;
  title: string;
  children: ReactNode;
  onOpenChange: (open: boolean) => void;
  footer?: ReactNode;
};

export function Dialog({ open, title, children, onOpenChange, footer }: Props) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <button className="absolute inset-0 bg-black/50" aria-label="关闭" onClick={() => onOpenChange(false)} />
      <div role="dialog" className="relative w-[min(480px,92vw)] rounded-md border border-[var(--line)] bg-[var(--surface)] p-4 shadow-lg">
        <h2 className="mb-2 text-sm font-semibold">{title}</h2>
        <div className="text-sm text-[var(--muted)]">{children}</div>
        <div className="mt-4 flex justify-end gap-2">
          {footer ?? <Button size="sm" onClick={() => onOpenChange(false)}>关闭</Button>}
        </div>
      </div>
    </div>
  );
}
