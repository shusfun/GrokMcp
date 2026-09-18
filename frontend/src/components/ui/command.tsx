import { Input } from "./input";
import { cn } from "../../lib/cn";

export function Command({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return <Input className={cn("min-w-[160px] flex-1", className)} value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} />;
}
