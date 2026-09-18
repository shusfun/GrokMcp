import { formatElapsed } from "../lib/jobs";

export function DurationText({ seconds }: { seconds: number }) {
  return <span className="font-mono tabular-nums text-[var(--muted)]">{formatElapsed(seconds)}</span>;
}
